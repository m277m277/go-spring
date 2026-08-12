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
	"context"
	"errors"

	"github.com/allegro/bigcache/v3"
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metricsMeter is the OTel meter all bigcache instruments register under. It
// follows the starter's module path, matching the convention used by other
// starters (e.g. starter-gin's "go-spring.org/starter-gin").
const metricsMeter = "go-spring.org/starter-bigcache"

// statReader reads one snapshot value from a BigCache instance.
type statReader func(*bigcache.BigCache) int64

// statInstrument describes one gauge to register.
type statInstrument struct {
	name string
	desc string
	read statReader
}

// statInstruments lists the bigcache statistics surfaced as gauges. The five
// counters come from Stats() (cumulative); entries/capacity come from Len()/
// Capacity() (current). All are gauges rather than counters because
// [bigcache.BigCache.ResetStats] can reset the counters, breaking monotonicity.
var statInstruments = []statInstrument{
	{name: "bigcache.hits", desc: "Number of successfully found keys", read: func(c *bigcache.BigCache) int64 { return c.Stats().Hits }},
	{name: "bigcache.misses", desc: "Number of not found keys", read: func(c *bigcache.BigCache) int64 { return c.Stats().Misses }},
	{name: "bigcache.delete_hits", desc: "Number of successfully deleted keys", read: func(c *bigcache.BigCache) int64 { return c.Stats().DelHits }},
	{name: "bigcache.delete_misses", desc: "Number of not deleted keys", read: func(c *bigcache.BigCache) int64 { return c.Stats().DelMisses }},
	{name: "bigcache.collisions", desc: "Number of key hash collisions", read: func(c *bigcache.BigCache) int64 { return c.Stats().Collisions }},
	{name: "bigcache.entries", desc: "Current number of stored entries", read: func(c *bigcache.BigCache) int64 { return int64(c.Len()) }},
	{name: "bigcache.capacity", desc: "Maximum number of entries the cache can hold", read: func(c *bigcache.BigCache) int64 { return int64(c.Capacity()) }},
}

// registerMetrics registers OTel observable gauges that surface a BigCache
// instance's statistics (hits, misses, collisions) and capacity, labeled with
// the instance name so multiple instances (e.g. "hot", "cold") are
// distinguishable in a metrics backend. Each gauge pulls its value on
// collection via a callback - no per-operation overhead and no background
// goroutine.
//
// When starter-otel is imported the gauges are exported through the global
// meter provider it installs (prometheus pull, OTLP, ...); when it is absent
// the OTel globals are no-ops, so this call is safe and cheap to always make.
func registerMetrics(name string, c *bigcache.BigCache) {
	meter := otel.Meter(metricsMeter)
	attrs := metric.WithAttributes(attribute.String("cache.name", name))
	for _, inst := range statInstruments {
		_, _ = meter.Int64ObservableGauge(inst.name,
			metric.WithDescription(inst.desc),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(inst.read(c), attrs)
				return nil
			}),
		)
	}
}

// --- per-operation observe wrapper -------------------------------------------
//
// Cache wraps *bigcache.BigCache so Get/Set/Delete flow through the
// shared observe kit (trace span + duration/in-flight metric + access log), in
// addition to the cache-stat gauges above. bigcache is an in-process heap cache
// with no network, so the spans are root spans (no caller context to link) and
// the durations are sub-microsecond - the value is per-key access visibility and
// a uniform signal vocabulary with the other client starters. It embeds the real
// client, so Reset/Stats/Len/Capacity/Close are promoted unchanged.
//
// The type is exported because bigcache (unlike go-redis or gorm) offers no
// hook/plugin extension point, so the only way to observe per-operation traffic
// is to hold the wrapper itself. Apps therefore inject *Cache rather
// than *bigcache.BigCache; the embedded field is available for any third-party
// API that needs the raw client.

type Cache struct {
	*bigcache.BigCache
	obs *observe.Observer

	// Resilience is field-injected (gs.Dync, hot-reloadable) so the protection
	// policy can change at runtime without a restart; Init subscribes
	// OnChanged to Refresh the executor. Observability is the startup access-log
	// config (not hot). These replace the old Config.Resilience/Observability
	// fields so the wrapper bean owns its own protection policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	// name is the instance name (the spring.bigcache.<name> map key), used for
	// the resilience resource label. Set by newClient; Init reads it.
	name string

	// exec is the resilience executor protecting Get/Set/Delete, set by
	// Init when resilience is enabled. nil on an unarmed client, in
	// which case guard runs the operation directly with no policy overhead.
	exec resilience.Executor
	// resource is the resilience resource key ("bigcache:<instance-name>")
	// exec scopes limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// Init is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the observe.Observer (needs
// Observability) and, when resilience is enabled, the executor (needs the
// Resilience policy) + arms guard + subscribes to policy changes for hot Refresh.
func (c *Cache) Init() error {
	c.obs = observe.NewClient("bigcache", c.Observability)
	rc := c.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	exec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "bigcache", c.Observability)
	c.exec = exec
	c.resource = resilience.ResourceLabel("bigcache", c.name)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh resets per-resource state (the intended semantic
	// of a threshold change - old failure counts were under the old policy).
	c.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}

// Close releases the resilience executor (if armed) and the underlying BigCache.
// It is the gs destroy method.
func (c *Cache) Destroy() error {
	if c.exec != nil {
		_ = c.exec.Close()
	}
	return c.BigCache.Close()
}

// guard runs fn under the resilience executor when armed, and directly otherwise.
// bigcache.ErrEntryNotFound is a cache miss — a normal, expected outcome — so it is
// treated as success for the breaker/retry (mirroring how go-redis treats
// redis.Nil and gorm treats ErrRecordNotFound). A rejection (rate-limited /
// circuit-open / bulkhead-full) is returned as the executor's sentinel error; any
// other error from fn feeds the breaker and may be retried.
func (c *Cache) guard(ctx context.Context, fn func(context.Context) error) error {
	if c.exec == nil {
		return fn(ctx)
	}
	var callErr error
	execErr := c.exec.Execute(ctx, c.resource, func(ctx context.Context) error {
		callErr = fn(ctx)
		if callErr != nil && !errors.Is(callErr, bigcache.ErrEntryNotFound) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or cache miss
	})
	if execErr != nil {
		return execErr // rejected by protection, or propagated failure
	}
	return callErr
}

func (c *Cache) Get(key string) ([]byte, error) {
	_, sp := c.obs.Start(context.Background(), "get", key)
	var v []byte
	err := c.guard(context.Background(), func(ctx context.Context) error {
		var e error
		v, e = c.BigCache.Get(key)
		return e
	})
	sp.End(err)
	return v, err
}

func (c *Cache) Set(key string, entry []byte) error {
	_, sp := c.obs.Start(context.Background(), "set", key)
	err := c.guard(context.Background(), func(ctx context.Context) error {
		return c.BigCache.Set(key, entry)
	})
	sp.End(err)
	return err
}

func (c *Cache) Delete(key string) error {
	_, sp := c.obs.Start(context.Background(), "delete", key)
	err := c.guard(context.Background(), func(ctx context.Context) error {
		return c.BigCache.Delete(key)
	})
	sp.End(err)
	return err
}
