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
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/data/cache"
	"go-spring.org/spring/gs"
	"go-spring.org/starter-redigo/bytecache"
	poolhealth "go-spring.org/starter-redigo/health"
	"go-spring.org/stdlib/flatten"
)

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

			// Contribute a health indicator for this instance unless the user
			// disabled it (health.enabled=false), injecting the pool just
			// registered above by name.
			if c.HealthEnabled {
				r.Provide(func(w *Pool) health.Indicator {
					return poolhealth.NewPoolHealth(name, w.Pool)
				}, gs.TagArg(name)).Name("redigo:" + name).Export(gs.As[health.Indicator]())
			}
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
