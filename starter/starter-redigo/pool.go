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

package StarterRedigo

import (
	"context"
	"io"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	"go-spring.org/log"
	observe "go-spring.org/observe"
	resilobserve "go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/errutil"
)

var starterTag = log.RegisterInfraTag("redigo", "")

// Pool is the wrapper bean redigo pools are injected as. It embeds
// the concrete *redis.Pool (so Get/Stats/etc. promote unchanged) and
// field-injects the resilience policy via gs.Dync so it hot-reloads on config
// change. newPool returns one; gs field-injects Resilience + Observability, then
// calls Init (InitMethod) to build the observer + executor and wrap
// the pool's Dial so every connection is an Conn that threads both through
// each command.
type Pool struct {
	*redis.Pool
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Fault         gs.Dync[fault.Config]      `value:"${fault:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	cfg      Config // address fields feed the resilience resource label
	obs      *observe.Observer
	exec     resilience.Executor // nil when neither resilience nor fault is enabled
	faultInj *fault.Injector     // non-nil only when fault was enabled at startup
	hook     CommandInterceptor  // nil when no per-command interceptor is registered
	resource string              // resilience resource label (stable per pool)
	stop     io.Closer           // driver-supplied teardown (e.g. discovery resolver watch)
}

// newPool creates a Redis pool based on the provided configuration, wrapped in
// a Pool so gs can field-inject resilience + observability and
// Init (InitMethod) can arm them (build the observer + executor and
// wrap the pool's Dial with an Conn). The startup ping here runs on the raw
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
	w := &Pool{Pool: pool, cfg: c, stop: stop, hook: interceptor}

	// Fail fast (opt-in): the redigo pool dials lazily, so when StartupPing is
	// set, dial one connection directly and PING it at startup. A misconfigured
	// address or unreachable server then surfaces during boot rather than on the
	// first request. This still runs on the raw Dial — Init (which wraps Dial
	// with Conn) is called by gs only after newPool returns. See startupPing
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

// Init is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It is a thin orchestrator: build the observer, arm the
// resilience executor (no-op when disabled), then wrap the pool's Dial so every
// connection handed out is an Conn that threads both through each command.
// The per-step detail lives just below in this file: setupResilience and setupDial.
func (o *Pool) Init() error {
	if o.cfg.ObserveEnabled {
		o.obs = observe.NewClient("redis", o.Observability)
	}
	if err := o.setupResilience(); err != nil {
		return err
	}
	o.setupDial()
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

// setupResilience builds the executor stack from the (hot-reloadable) resilience
// and fault configs and stores it on the pool so every Conn threads each command
// through it. It is a no-op when neither is enabled, leaving o.exec nil and the
// inner conn to be called directly.
//
// The stack is observe( fault( rawExec ) ): fault wraps the raw executor's
// operation fn so injected failures land INSIDE the retry/breaker loop (and so
// are observed), and observe sits outermost so trips / rejects / retries emit
// span + counter + histogram + access log (the resilience core emits none). When
// resilience is disabled but fault is enabled, a zero-policy raw executor still
// carries the fault layer (single attempt, no retry/breaker) so fault does not
// depend on resilience being on. fault must be enabled at startup for its wrap
// layer to exist; once present, Rate/Error/Latency/Enabled hot-toggle at runtime.
//
// On a config change the bound policy is adopted without a restart via the
// executor's Refresh seam. Called by Init (the gs
// InitMethod) after the observer is built.
func (o *Pool) setupResilience() error {
	rc := o.Resilience.Value()
	fc := o.Fault.Value()
	if !rc.Enabled && !fc.Enabled {
		return nil
	}
	rawExec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec := rawExec
	if fc.Enabled {
		o.faultInj = fault.NewInjector(fc)
		exec = fault.WrapExecutor(rawExec, o.faultInj)
	}
	exec = resilobserve.WrapExecutor(exec, "redigo", o.Observability)
	o.exec = exec
	// Scope limiter/breaker state per Redis instance (not per command): fall back
	// across the address fields via the shared [resilience.ResourceLabel] helper.
	o.resource = resilience.ResourceLabel("redigo", o.cfg.ServiceName, o.cfg.Addr)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh propagates through the observe + fault wraps.
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		_ = exec.Refresh(new.Policy())
	})
	if o.faultInj != nil {
		o.Fault.OnChanged(func(new, _ fault.Config) {
			o.faultInj.SetConfig(new)
		})
	}
	return nil
}

// setupDial wraps the pool's Dial / DialContext so every connection handed out
// is an Conn that instruments each command through the shared observe kit
// (trace span + duration/in-flight metric + access log) and, when resilience is
// armed, through the executor. It is called by Init (the gs
// InitMethod) after the observer + executor are built; the Conn captures them
// at install time, so a nil o.exec (resilience disabled) means the inner conn is
// called directly with no executor overhead.
//
// The kit rides the OTel globals (starter-otel) and the project log, so this is
// a near-zero-cost opt-in that needs no per-component adaptation: when
// starter-otel is absent, trace+metric are no-ops; the access log is gated by
// cfg.Level (default brief).
func (o *Pool) setupDial() {
	wrap := func(c redis.Conn) redis.Conn {
		return &Conn{Conn: c, obs: o.obs, exec: o.exec, resource: o.resource, interceptor: o.hook}
	}
	if d := o.Pool.Dial; d != nil {
		o.Pool.Dial = func() (redis.Conn, error) {
			c, err := d()
			if err != nil {
				return nil, err
			}
			return wrap(c), nil
		}
	}
	if d := o.Pool.DialContext; d != nil {
		o.Pool.DialContext = func(ctx context.Context) (redis.Conn, error) {
			c, err := d(ctx)
			if err != nil {
				return nil, err
			}
			return wrap(c), nil
		}
	}
}

// startupPing dials one bare connection and PINGs it so a misconfigured address
// or unreachable server surfaces during boot rather than on the first request.
//
// It uses pool.Dial (a non-pooled dial) instead of pool.Get: a conn borrowed via
// Get is returned to the idle pool on Close, and that happens *before*
// Init wraps pool.Dial with the Conn — so the stale raw conn would
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
