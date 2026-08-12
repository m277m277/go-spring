/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package StarterRedigo registers the redigo (gomodule/redigo) Redis client as a
// go-spring starter. Each entry under "${spring.redigo}" becomes one pooled
// client bean; the pool is wrapped by Pool, which layers
// observability (trace/metric/log via the shared observe kit) and resilience
// (rate-limit / circuit-breaker / retry / timeout via the resilience executor)
// onto every command. A "redigo" cache driver is also registered so a pool can
// be exposed as a cache.Cache.
package StarterRedigo

import (
	"context"
	"io"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/cloud/resilience"
	"go-spring.org/log"
	observe "go-spring.org/observe"
	resilobserve "go-spring.org/observe/resilience"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/data/cache"
	"go-spring.org/spring/gs"
	"go-spring.org/starter-redigo/bytecache"
	poolhealth "go-spring.org/starter-redigo/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

var starterTag = log.RegisterInfraTag("redigo", "")

func init() {
	// Register multiple Redis clients as a group, one per entry under
	// "${spring.redigo}". A gs.Module (rather than gs.Group) is used so each
	// instance's pool bean can be paired with a health.Indicator registered under
	// the same name — and to attach the file:line of this registration to the
	// bean for diagnostics.
	gs.Module(gs.OnProperty("spring.redigo"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.redigo}", func(name string, c Config) error {

			r.Provide(newPool, gs.IndexArg(1, gs.ValueArg(c))).
				Name(name).
				Init((*Pool).Init).
				Destroy((*Pool).Destroy)

			// Contribute a health indicator for this instance, injecting the
			// pool just registered above by name.
			r.Provide(func(w *Pool) health.Indicator {
				return poolhealth.NewPoolHealth(name, w.Pool)
			}, gs.TagArg(name)).Name("redigo:" + name).Export(gs.As[health.Indicator]())
			return nil
		})
	})

	// init registers the "redigo" cache driver so a *redis.Pool registered under
	// ${spring.redigo} can be exposed as a cache.Cache via:
	//
	//	spring.cache.<name>.driver = redigo:<redigo-instance-name>
	//
	// The beanID selects which pool bean to wrap; the implementation lives in
	// starter-redigo/bytecache.
	cache.RegisterDriver("redigo", func(beanID string) gs.ModuleFunc {
		return func(r gs.BeanProvider, p flatten.Storage) error {
			r.Provide(func(w *Pool) *cache.Cache {
				return &cache.Cache{ByteCache: bytecache.NewByteCache(w.Pool)}
			}, gs.TagArg(beanID)).Name(beanID)
			return nil
		}
	})
}

// newPool creates a Redis pool based on the provided configuration, wrapped in
// a Pool so gs can field-inject resilience + observability and
// Init (InitMethod) can arm them (build the observer + executor and
// wrap the pool's Dial with an obsConn). The startup ping here runs on the raw
// pool before the Dial wrapper is installed — a bare connectivity probe, not an
// instrumented command.
func newPool(ctx *gs.ContextProvider, c Config) (*Pool, error) {

	log.Debugf(ctx.Context, starterTag, "creating redigo client, addr=%s service-name=%s", c.Addr, c.ServiceName)

	if err := errutil.RequireAny("redis",
		errutil.Field{Name: "addr", Value: c.Addr},
		errutil.Field{Name: "service-name", Value: c.ServiceName},
	); err != nil {
		return nil, err
	}

	d, ok := driverRegistry[c.Driver]
	if !ok {
		log.Errorf(ctx.Context, starterTag, "redigo driver not found: %s", c.Driver)
		return nil, errutil.Explain(nil, "redis driver not found: %s", c.Driver)
	}

	pool, stop, err := d.CreateClient(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "redigo: create client failed: %v", err)
		return nil, errutil.Explain(err, "failed to create redis client")
	}

	// Wrap the pool first so any failure below tears it down via Close (which
	// also stops the discovery resolver watch) — matching starter-go-redis.
	w := &Pool{Pool: pool, cfg: c, stop: stop}

	// Fail fast (opt-in): the redigo pool dials lazily, so when StartupPing is
	// set, dial one connection directly and PING it at startup. A misconfigured
	// address or unreachable server then surfaces during boot rather than on the
	// first request. This still runs on the raw Dial — Init (which wraps Dial
	// with obsConn) is called by gs only after newPool returns. See startupPing
	// for why it dials directly instead of via pool.Get.
	if c.StartupPing {
		if err := startupPing(ctx.Context, w.Pool); err != nil {
			_ = w.Close() // stop discovery watch + close pool
			return nil, err
		}
	}

	log.Infof(ctx.Context, starterTag, "redigo client initialized, addr=%s", c.Addr)
	return w, nil
}

// Pool is the wrapper bean redigo pools are injected as. It embeds
// the concrete *redis.Pool (so Get/Stats/etc. promote unchanged) and
// field-injects the resilience policy via gs.Dync so it hot-reloads on config
// change. newPool returns one; gs field-injects Resilience + Observability, then
// calls Init (InitMethod) to build the observer + executor and wrap
// the pool's Dial so every connection is an obsConn.
type Pool struct {
	*redis.Pool
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	cfg      Config // address fields feed the resilience resource label
	obs      *observe.Observer
	exec     resilience.Executor // nil when resilience is disabled
	resource string              // resilience resource label (stable per pool)
	stop     io.Closer           // driver-supplied teardown (e.g. discovery resolver watch)
}

// Init is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It is a thin orchestrator: build the observer, arm the
// resilience executor (no-op when disabled), then wrap the pool's Dial so every
// connection handed out is an obsConn that threads both through each command.
// The per-step detail lives next to its concern: armExecutor in resilience.go,
// wrapDial in observability.go.
func (o *Pool) Init() error {
	o.obs = observe.NewClient("redis", o.Observability)
	if err := o.armExecutor(); err != nil {
		return err
	}
	o.wrapDial()
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor (if armed),
// stops any driver-supplied teardown (the discovery resolver watch, when armed),
// and closes the pool.
func (o *Pool) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	_ = o.stop.Close()
	return o.Pool.Close()
}

// startupPing dials one bare connection and PINGs it so a misconfigured address
// or unreachable server surfaces during boot rather than on the first request.
//
// It uses pool.Dial (a non-pooled dial) instead of pool.Get: a conn borrowed via
// Get is returned to the idle pool on Close, and that happens *before*
// Init wraps pool.Dial with the obsConn — so the stale raw conn would
// later be handed out with a nil executor and silently bypass resilience.
// Dialing directly keeps it out of the pool. Only runs when Config.StartupPing
// is set.
func startupPing(ctx context.Context, pool *redis.Pool) error {
	conn, err := pool.Dial()
	if err != nil {
		log.Errorf(ctx, starterTag, "redigo: startup ping failed: %v", err)
		return errutil.Explain(err, "redis: startup ping failed")
	}
	_, pingErr := conn.Do("PING")
	_ = conn.Close()
	if pingErr != nil {
		log.Errorf(ctx, starterTag, "redigo: startup ping failed: %v", pingErr)
		return errutil.Explain(pingErr, "redis: startup ping failed")
	}
	return nil
}

// armExecutor builds the resilience executor from the (hot-reloadable) policy
// when resilience is enabled, wraps it so breaker trips / rejects / retries emit
// span + counter + histogram + access log (the resilience core emits none), and
// stores it on the pool so every obsConn threads each command through it. It is
// a no-op when resilience is disabled (o.Resilience.Value().Enabled == false),
// leaving o.exec nil and the inner conn to be called directly.
//
// On a config change the bound policy is adopted without a restart via the
// executor's RefreshableExecutor seam. Called by Init (the gs
// InitMethod) after the observer is built.
func (o *Pool) armExecutor() error {
	rc := o.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	exec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "redigo", o.Observability)
	o.exec = exec
	// Scope limiter/breaker state per Redis instance (not per command): fall back
	// across the address fields via the shared [resilience.ResourceLabel] helper.
	o.resource = resilience.ResourceLabel("redigo", o.cfg.ServiceName, o.cfg.Addr)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart.
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}
