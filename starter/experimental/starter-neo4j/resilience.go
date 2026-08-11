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

package StarterNeo4j

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/experimental/resilience"
	"go-spring.org/spring/gs"
)

// ObservedNeo4jDriver is the wrapper bean Neo4j drivers are injected as. It
// embeds the neo4j.DriverWithContext interface (so every driver method promotes
// unchanged) and field-injects the resilience policy via gs.Dync so it
// hot-reloads on config change. newClient returns one; gs field-injects
// Resilience + Observability, then calls ApplyResilience (InitMethod) to build
// the executor for the call-site guard.
//
// The Neo4j seam: the driver's ExecuteQuery is a package-level function (not a
// method on the driver), so there is no transport / dialer / hook to intercept —
// the only viable insertion point is a call-site guard. Applications that call
// [Query] (the instrumented drop-in for neo4j.ExecuteQuery) or
// [RunWithResilience] route through this wrapper's executor automatically when
// resilience is enabled; code that drives sessions directly is untouched (and
// un-protected) unless it calls [RunWithResilience].
type ObservedNeo4jDriver struct {
	neo4j.DriverWithContext
	// Resilience is field-injected (gs.Dync, hot-reloadable); Observability is
	// the startup access-log config (not hot). These replace the old
	// Config.Resilience/Observability fields so the wrapper bean owns its own
	// protection + observability policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.LogConfig          `value:"${observability:=}"`

	// cfg is the connection config, retained for the resilience resource label.
	cfg Config
	// resolver is the discovery watch behind a service-name driver (nil when
	// direct/mesh); Close stops it on shutdown.
	resolver *discovery.Resolver
	// exec is the resilience executor protecting queries, set by ApplyResilience
	// when resilience is enabled. nil on an unarmed driver.
	exec resilience.Executor
	// resource is the resilience resource key ("neo4j:<...>") exec scopes
	// limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// ApplyResilience is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the executor when
// resilience is enabled and stores it on the wrapper so [Query] /
// [RunWithResilience] can route through it, and subscribes to policy changes
// for hot Refresh.
func (o *ObservedNeo4jDriver) ApplyResilience() error {
	rc := o.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	drv, err := resilience.MustGetDriver(rc.Driver)
	if err != nil {
		return err
	}
	exec, err := drv.NewExecutor(rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "neo4j", o.Observability)
	o.exec = exec
	o.resource = resourceLabel(o.cfg)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh resets per-resource state (the intended semantic
	// of a threshold change - old failure counts were under the old policy).
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}

// CloseDriver is the gs destroy method: it closes the resilience executor (if
// armed), stops any discovery watch, and closes the underlying driver.
//
// It is deliberately NOT named Close: the embedded neo4j.DriverWithContext
// already exposes Close(context.Context), and shadowing it with a different
// signature would stop the wrapper from satisfying the interface (and thus from
// being passed to neo4j.ExecuteQuery / [Query]). This teardown is referenced
// explicitly from the gs registration.
func (o *ObservedNeo4jDriver) CloseDriver() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.resolver)
	return o.DriverWithContext.Close(context.Background())
}

// queryResilience returns the executor + resource armed on a wrapped driver, or
// (nil, "") when driver is unwrapped or resilience is disabled.
func queryResilience(driver neo4j.DriverWithContext) (resilience.Executor, string) {
	if w, ok := driver.(*ObservedNeo4jDriver); ok {
		return w.exec, w.resource
	}
	return nil, ""
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// driver, so limiter and breaker state is scoped per Neo4j instance rather than
// per query. It falls back to the URI via the shared [resilience.ResourceLabel]
// helper.
func resourceLabel(c Config) string {
	return resilience.ResourceLabel("neo4j", c.ServiceName, c.URI)
}
