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

package StarterMongoDB

import (
	"context"
	"sync/atomic"

	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Client is the wrapper bean MongoDB clients are injected as. It
// embeds the concrete *mongo.Client (so every driver method promotes
// unchanged) and field-injects the resilience policy via gs.Dync so it
// hot-reloads on config change. newClient returns one; gs field-injects
// Resilience + Observability, then calls Init (InitMethod) to build
// the observer and, when resilience is enabled, the executor + wrapped dialer.
//
// The resilience seam is the dial layer: the mongo driver v2 exposes no single
// per-operation hook comparable to go-redis's ProcessHook, so the cleanest
// insertion point is the dialer (a breaker trips on connection failures, a
// limiter caps connection churn, a bulkhead bounds concurrent dials).
// Already-open connections run at full speed — this is connection-level
// protection, not per-command. newClient installs a shared dialer instance and
// Init mutates its dial function to the resilience-wrapped one, so
// the swap takes effect without rebuilding the client.
type Client struct {
	*mongo.Client
	// Resilience is field-injected (gs.Dync, hot-reloadable); Observability is
	// the startup access-log config (not hot). These replace the old
	// Config.Resilience/Observability fields so the wrapper bean owns its own
	// protection + observability policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	// cfg is the connection config, retained for the resilience resource label.
	cfg Config
	// dialer is the shared dialer handed to the driver; Init swaps
	// its dial field to the resilience-wrapped function.
	dialer *dialerWrapper
	// resolver is the discovery watch behind a service-name client (nil when
	// direct/mesh); Close stops it on shutdown.
	resolver *discovery.Resolver
	// obs is the observe observer built by Init from the injected
	// Observability; the command monitor reads it lazily.
	obs atomic.Pointer[observe.Observer]
	// exec is the resilience executor protecting dials, set by Init
	// when resilience is enabled. nil on an unarmed client.
	exec resilience.Executor
	// resource is the resilience resource key ("mongodb:<...>") exec scopes
	// limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// Init is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the observe.Observer (needs
// Observability) and, when resilience is enabled, the executor + the
// resilience-wrapped dial function (needs the Resilience policy), swaps it into
// the shared dialer, and subscribes to policy changes for hot Refresh.
func (o *Client) Init() error {
	o.obs.Store(observe.NewClient("mongodb", o.Observability))
	rc := o.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	exec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "mongodb", o.Observability)
	o.exec = exec
	o.resource = resilience.ResourceLabel("mongodb", o.cfg.ServiceName, o.cfg.URI)
	// Wrap the current (plain/discovery) dial with the policy and swap it into
	// the shared dialer the driver already holds.
	baseDial := o.dialer.dial
	o.dialer.dial = resilience.NewDialer(baseDial, exec, o.resource)
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

// Destroy is the gs destroy method: it closes the resilience executor (if armed),
// stops any discovery watch, and disconnects the underlying client.
func (o *Client) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.resolver)
	return o.Client.Disconnect(context.Background())
}
