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

package StarterBigCache

import (
	"runtime"

	"go-spring.org/log"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/data/cache"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-bigcache/health"
	"go-spring.org/starter-bigcache/bytecache"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

var starterTag = log.RegisterInfraTag("bigcache", "")

func init() {
	// Register multiple BigCache instances as a group, one per entry under
	// "${spring.bigcache}". A gs.Module (rather than gs.Group) is used so each
	// instance's name is available to label its OTel metrics - and to attach
	// the file:line of this registration to the bean for diagnostics.
	//
	// BigCache spawns a background eviction goroutine, so Close must be called
	// on shutdown to release it - the destroy callback handles that.
	_, file, line, _ := runtime.Caller(0)
	gs.Module(gs.OnProperty("spring.bigcache"), func(r gs.BeanProvider, p flatten.Storage) error {
		var m map[string]Config
		if err := conf.Bind(p, &m, "${spring.bigcache}"); err != nil {
			return err
		}
		for name, c := range m {
			// IndexArg places name (index 1) and c (index 2) explicitly, leaving
			// index 0 (*gs.ContextProvider) to be autowired - the documented
			// pattern for a ctor whose first param is ContextProvider.
			b := r.Provide(newClient,
				gs.IndexArg(1, gs.ValueArg(name)),
				gs.IndexArg(2, gs.ValueArg(c)),
			).Name(name).InitMethod("ApplyResilience").Destroy((*ObservedBigCache).Close)
			b.SetFileLine(file, line)
			// Contribute a health indicator for this instance, injecting the
			// client just registered above by name.
			h := r.Provide(func(c *ObservedBigCache) health.Indicator { return health2.NewBigCacheHealth(name, c.BigCache) }, gs.TagArg(name)).Name(name).Export(gs.As[health.Indicator]())
			h.SetFileLine(file, line)
		}
		return nil
	})

	// init registers the "bigcache" cache driver so a *bigcache.BigCache registered
	// under ${spring.bigcache} can be exposed as a cache.Cache via:
	//
	//	spring.cache.<name>.driver = bigcache:<bigcache-instance-name>
	//
	// The beanID selects which BigCache bean to wrap; the implementation lives in
	// starter-bigcache/bytecache.
	cache.RegisterDriver("bigcache", func(beanID string) gs.ModuleFunc {
		return func(r gs.BeanProvider, p flatten.Storage) error {
			r.Provide(func(c *ObservedBigCache) *cache.Cache {
				return &cache.Cache{ByteCache: bytecache.NewByteCache(c.BigCache)}
			}, gs.TagArg(beanID)).Name(beanID)
			return nil
		}
	})
}

// newClient creates a new BigCache instance based on the provided configuration,
// wrapped so Get/Set/Delete flow through the observe kit, and registers OTel
// gauges for its statistics, labeled by the instance name.
func newClient(ctx *gs.ContextProvider, name string, c Config) (*ObservedBigCache, error) {
	log.Debugf(ctx.Context, starterTag, "creating bigcache instance, name=%s shards=%d max-size=%d", name, c.Shards, c.MaxEntrySize)

	d, ok := driverRegistry[c.Driver]
	if !ok {
		log.Errorf(ctx.Context, starterTag, "bigcache driver not found: %s", c.Driver)
		return nil, errutil.Explain(nil, "bigcache driver not found: %s", c.Driver)
	}
	client, err := d.CreateClient(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "bigcache: create instance failed: %v", err)
		return nil, errutil.Explain(err, "failed to create bigcache instance")
	}
	// Surface hits/misses/collisions/capacity as OTel gauges. Safe no-op when
	// starter-otel is absent (the OTel globals are no-ops then).
	registerMetrics(name, client)
	log.Infof(ctx.Context, starterTag, "bigcache instance initialized, name=%s shards=%d", name, c.Shards)
	// Return the wrapper; gs field-injects Resilience (gs.Dync, hot-reloadable)
	// + Observability after this returns, then calls ApplyResilience (InitMethod)
	// to build the observer + executor.
	return &ObservedBigCache{BigCache: client, name: name}, nil
}
