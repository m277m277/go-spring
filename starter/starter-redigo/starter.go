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
	"runtime"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/log"
	"go-spring.org/spring/cloud/actuator/health"
	"go-spring.org/spring/cloud/discovery"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/data/cache"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-redigo/health"
	"go-spring.org/starter-redigo/bytecache"
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
	_, file, line, _ := runtime.Caller(0)
	gs.Module(gs.OnProperty("spring.redigo"), func(r gs.BeanProvider, p flatten.Storage) error {
		var m map[string]Config
		if err := conf.Bind(p, &m, "${spring.redigo}"); err != nil {
			return err
		}
		for name, c := range m {
			b := r.Provide(newClient, gs.ValueArg(c)).Name(name).Destroy(destroyClient)
			b.SetFileLine(file, line)
			// Contribute a health indicator for this instance, injecting the
			// pool just registered above by name.
			h := r.Provide(health2.NewPoolHealth, gs.ValueArg(name), gs.TagArg(name)).Export(gs.As[health.Indicator]())
			h.SetFileLine(file, line)
		}
		return nil
	})
}

// init registers the "redigo" cache driver so a *redis.Pool registered under
// ${spring.redigo} can be exposed as a cache.Cache via:
//
//	spring.cache.<name>.driver = redigo:<redigo-instance-name>
//
// The beanID selects which pool bean to wrap; the implementation lives in
// starter-redigo/bytecache.
func init() {
	cache.RegisterDriver("redigo", func(beanID string) gs.ModuleFunc {
		return func(r gs.BeanProvider, p flatten.Storage) error {
			r.Provide(func(pool *redis.Pool) *cache.Cache {
				return &cache.Cache{ByteCache: bytecache.NewByteCache(pool)}
			}, gs.TagArg(beanID)).Name(beanID)
			return nil
		}
	})
}

// newClient creates a new Redis client based on the provided configuration.
func newClient(ctx *gs.ContextProvider, c Config) (*redis.Pool, error) {

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
	pool, err := d.CreateClient(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "redigo: create client failed: %v", err)
		return nil, errutil.Explain(err, "failed to create redis client")
	}
	// Attach transparent trace+metric+log instrumentation to every command on
	// the pool (replaces the manual StartRedisSpan call-site helper).
	applyObservability(pool, c.Observability)
	// Fail fast: the redigo pool dials lazily, so borrow one connection and
	// PING it at startup. A misconfigured address or unreachable server then
	// surfaces during boot rather than on the first request.
	conn := pool.Get()
	defer func() { _ = conn.Close() }()
	if _, err := conn.Do("PING"); err != nil {
		log.Errorf(ctx.Context, starterTag, "redigo: startup ping failed: %v", err)
		_ = pool.Close()
		return nil, errutil.Explain(err, "redis: startup ping failed")
	}
	log.Infof(ctx.Context, starterTag, "redigo client initialized, addr=%s", c.Addr)
	return pool, nil
}

// destroyClient closes the Redis pool and stops any discovery watch behind it.
func destroyClient(pool *redis.Pool) error {
	if v, ok := liveDialers.LoadAndDelete(pool); ok {
		_ = v.(*discovery.Resolver).Stop()
		log.Debugf(context.Background(), starterTag, "redigo client destroyed, discovery resolver stopped")
	}
	return pool.Close()
}
