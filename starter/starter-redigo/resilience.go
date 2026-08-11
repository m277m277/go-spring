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

	"github.com/gomodule/redigo/redis"
	"go-spring.org/log"
	observe "go-spring.org/observe"
	resilobserve "go-spring.org/observe-resilience"
	"go-spring.org/spring/cloud/discovery"
	"go-spring.org/spring/experimental/cloud/resilience"
	"go-spring.org/spring/gs"
)

// ObservedRedisPool is the wrapper bean redigo pools are injected as. It embeds
// the concrete *redis.Pool (so Get/Stats/etc. promote unchanged) and
// field-injects the resilience policy via gs.Dync so it hot-reloads on config
// change. newClient returns one; gs field-injects Resilience + Observability,
// then calls ApplyResilience (InitMethod) to build the observer + executor and
// wrap the pool's Dial so every connection is an obsConn.
type ObservedRedisPool struct {
	*redis.Pool
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.LogConfig          `value:"${observability:=}"`

	cfg      Config // for resourceLabel (address fields)
	obs      *observe.Observer
	exec     resilience.Executor // nil when resilience is disabled
	resource string              // resilience resource label (stable per pool)
}

// ApplyResilience is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It builds the observer, then the executor when resilience is
// enabled (subscribing OnChanged for hot Refresh), and finally wraps the pool's
// Dial so every connection handed out is an obsConn that threads both through
// each command.
func (o *ObservedRedisPool) ApplyResilience() error {
	o.obs = observe.NewClient("redis", o.Observability)
	rc := o.Resilience.Value()
	if rc.Enabled {
		drv, err := resilience.MustGetDriver(rc.Driver)
		if err != nil {
			return err
		}
		exec, err := drv.NewExecutor(rc.Policy())
		if err != nil {
			return err
		}
		// Wrap so breaker trips / rejects / retries emit span + counter + histogram
		// + access log (the resilience core emits none). nil-safe, no-op without
		// starter-otel.
		exec = resilobserve.WrapExecutor(exec, "redigo", o.Observability)
		o.exec = exec
		o.resource = resourceLabel(o.cfg)
		// Hot-reload: when the bound resilience config changes, adopt the new
		// policy without a restart.
		o.Resilience.OnChanged(func(new, _ resilience.Config) {
			if r, ok := exec.(resilience.RefreshableExecutor); ok {
				_ = r.Refresh(new.Policy())
			}
		})
	}
	o.installObsConn()
	return nil
}

// Close is the gs destroy method: closes the resilience executor (if armed),
// stops any discovery watch behind the pool, and closes the pool.
func (o *ObservedRedisPool) Close() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	if v, ok := liveDialers.LoadAndDelete(o.Pool); ok {
		_ = v.(*discovery.Resolver).Stop()
		log.Debugf(context.Background(), starterTag, "redigo client destroyed, discovery resolver stopped")
	}
	return o.Pool.Close()
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// pool, so limiter and breaker state is scoped per Redis instance rather than
// per command. It falls back across the address fields via the shared
// [resilience.ResourceLabel] helper.
func resourceLabel(c Config) string {
	return resilience.ResourceLabel("redigo", c.ServiceName, c.Addr)
}
