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
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
)

// Client is the wrapper bean Neo4j drivers are injected as. It
// embeds the neo4j.DriverWithContext interface (so every driver method promotes
// unchanged) and field-injects the resilience policy via gs.Dync so it
// hot-reloads on config change. newClient returns one; gs field-injects
// Resilience + Observability, then calls Init (InitMethod) to build
// the executor for the call-site guard.
//
// The Neo4j seam: the driver's ExecuteQuery is a package-level function (not a
// method on the driver), so there is no transport / dialer / hook to intercept —
// the only viable insertion point is a call-site guard. Applications that call
// [Query] (the instrumented drop-in for neo4j.ExecuteQuery) or
// [RunWithResilience] route through this wrapper's executor automatically when
// resilience is enabled; code that drives sessions directly is untouched (and
// un-protected) unless it calls [RunWithResilience].
type Client struct {
	neo4j.DriverWithContext
	// Resilience is field-injected (gs.Dync, hot-reloadable); Observability is
	// the startup access-log config (not hot). These replace the old
	// Config.Resilience/Observability fields so the wrapper bean owns its own
	// protection + observability policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Fault         gs.Dync[fault.Config]      `value:"${fault:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	// cfg is the connection config, retained for the resilience resource label.
	cfg Config
	// resolver is the discovery watch behind a service-name driver (nil when
	// direct/mesh); Close stops it on shutdown.
	resolver *discovery.Resolver
	// exec is the resilience executor protecting queries, set by Init
	// when resilience is enabled. nil on an unarmed driver.
	exec resilience.Executor
	// faultInj is the fault injector when fault is enabled; nil otherwise.
	// It sits between the raw executor and the observe wrap so injected faults
	// are observable. Set by Init when fault is enabled.
	faultInj *fault.Injector
	// resource is the resilience resource key ("neo4j:<...>") exec scopes
	// limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// Init is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the executor when
// resilience is enabled and stores it on the wrapper so [Query] /
// [RunWithResilience] can route through it, and subscribes to policy changes
// for hot Refresh.
func (o *Client) Init() error {
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
	exec = resilobserve.WrapExecutor(exec, "neo4j", o.Observability)
	o.exec = exec
	o.resource = resilience.ResourceLabel("neo4j", o.cfg.ServiceName, o.cfg.URI)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh resets per-resource state (the intended semantic
	// of a threshold change - old failure counts were under the old policy).
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

// Destroy is the gs destroy method: it closes the resilience executor (if
// armed), stops any discovery watch, and closes the underlying driver.
//
// It is deliberately NOT named Close: the embedded neo4j.DriverWithContext
// already exposes Close(context.Context), and shadowing it with a different
// signature would stop the wrapper from satisfying the interface (and thus from
// being passed to neo4j.ExecuteQuery / [Query]). This teardown is referenced
// explicitly from the gs registration.
func (o *Client) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.resolver)
	return o.DriverWithContext.Close(context.Background())
}

// queryResilience returns the executor + resource armed on a wrapped driver, or
// (nil, "") when driver is unwrapped or resilience is disabled.
func queryResilience(driver neo4j.DriverWithContext) (resilience.Executor, string) {
	if w, ok := driver.(*Client); ok {
		return w.exec, w.resource
	}
	return nil, ""
}

