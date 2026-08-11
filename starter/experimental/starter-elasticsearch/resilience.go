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

package StarterElasticsearch

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/elastic/go-elasticsearch/v8"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/experimental/resilience"
	"go-spring.org/spring/gs"
)

// ObservedElasticClient is the wrapper bean Elasticsearch clients are injected
// as. It embeds the concrete *elasticsearch.Client (so every generated method
// promotes unchanged) and field-injects the resilience policy via gs.Dync so it
// hot-reloads on config change. newClient returns one; gs field-injects
// Resilience + Observability, then calls ApplyResilience (InitMethod) to build
// the observe transport + executor and swap them into the client's dynamic
// transport.
//
// The elasticsearch seam: the transport is fixed inside elasticsearch.Config at
// construction and cannot be swapped on the client afterwards. To preserve a
// hot-reloadable resilience policy, DefaultDriver installs a thin
// [dynamicTransport] (an atomic RoundTripper indirection) as the client's
// transport. ApplyResilience then builds the observe+resilience transport from
// the injected policy and swaps it into the dynamic transport — so the
// protection the client actually uses is dynamic even though the transport
// instance is not.
type ObservedElasticClient struct {
	*elasticsearch.Client
	// Resilience is field-injected (gs.Dync, hot-reloadable); Observability is
	// the startup access-log config (not hot). These replace the old
	// Config.Resilience/Observability fields so the wrapper bean owns its own
	// protection + observability policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.LogConfig          `value:"${observability:=}"`

	// cfg is the connection config, retained for the resilience resource label.
	cfg Config
	// dyn is the dynamic transport DefaultDriver installed; ApplyResilience
	// swaps the observe+resilience transport into it. nil for custom drivers.
	dyn *dynamicTransport
	// resolver is the discovery watch behind a service-name client (nil when
	// direct/mesh); Close stops it on shutdown.
	resolver *discovery.Resolver
	// exec is the resilience executor protecting requests, set by
	// ApplyResilience when resilience is enabled. nil on an unarmed client.
	exec resilience.Executor
	// resource is the resilience resource key ("elasticsearch:<...>") exec
	// scopes limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// ApplyResilience is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the observe transport
// (needs Observability) and, when resilience is enabled, the executor (needs
// the Resilience policy), wraps them, swaps the result into the client's
// dynamic transport, and subscribes to policy changes for hot Refresh.
func (o *ObservedElasticClient) ApplyResilience() error {
	obs := observe.NewClient("elasticsearch", o.Observability, observe.WithoutTrace())
	observeTransport := &obsTransport{base: http.DefaultTransport, obs: obs}
	rc := o.Resilience.Value()
	if !rc.Enabled {
		if o.dyn != nil {
			o.dyn.Swap(observeTransport)
		}
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
	// Wrap the executor with observe-resilience so circuit-breaker trips,
	// rate-limit rejects, bulkhead rejections and retries emit a span + call
	// counter (by outcome) + duration histogram + access log — the resilience
	// core deliberately emits none. nil-safe and near-zero-cost when
	// starter-otel is absent (the OTel globals are no-ops).
	exec = resilobserve.WrapExecutor(exec, "elasticsearch", o.Observability)
	o.exec = exec
	o.resource = resourceLabel(o.cfg)
	if o.dyn != nil {
		o.dyn.Swap(resilience.NewRoundTripper(observeTransport, exec,
			func(*http.Request) string { return o.resource }))
	}
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

// Close is the gs destroy method: it closes the resilience executor (if armed),
// stops any discovery watch, and closes the underlying client.
func (o *ObservedElasticClient) Close() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.resolver)
	return o.Client.Close(context.Background())
}

// dynamicTransports tracks the dynamic transport DefaultDriver installed for
// each client, so newClient can hand it to the wrapper for ApplyResilience to
// arm. The key is the *elasticsearch.Client value; only clients built by
// DefaultDriver appear here.
var dynamicTransports sync.Map // *elasticsearch.Client -> *dynamicTransport

// dynamicTransport is a thin http.RoundTripper indirection whose behavior can
// be swapped after construction. elasticsearch fixes the transport at
// construction time, so to keep resilience hot-reloadable the fixed transport
// is this indirection and ApplyResilience swaps in the observe+resilience
// transport (or the observe-only transport when resilience is disabled). Until
// ApplyResilience runs it passes straight through to http.DefaultTransport.
type dynamicTransport struct {
	cur atomic.Value // holds http.RoundTripper
}

func newDynamicTransport() *dynamicTransport {
	t := &dynamicTransport{}
	t.cur.Store(http.DefaultTransport)
	return t
}

// Swap atomically replaces the active round-tripper.
func (t *dynamicTransport) Swap(rt http.RoundTripper) {
	t.cur.Store(rt)
}

// RoundTrip delegates to the currently-active round-tripper.
func (t *dynamicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.cur.Load().(http.RoundTripper).RoundTrip(req)
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// client, so limiter and breaker state is scoped per Elasticsearch cluster
// rather than per request. It falls back across the address fields via the
// shared [resilience.ResourceLabel] helper.
func resourceLabel(c Config) string {
	first := ""
	if len(c.Addresses) > 0 {
		first = c.Addresses[0]
	}
	return resilience.ResourceLabel("elasticsearch", c.ServiceName, c.CloudID, first)
}
