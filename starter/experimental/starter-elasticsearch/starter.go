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
	"fmt"
	"runtime"

	"go-spring.org/log"
	"go-spring.org/spring/cloud/actuator/health"
	"go-spring.org/spring/cloud/discovery"
	"go-spring.org/spring/cloud/mesh"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-elasticsearch/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

func init() {
	// Register multiple Elasticsearch clients as a group, one per entry under
	// "${spring.elasticsearch}". A gs.Module (rather than gs.Group) is used so
	// each instance's *elasticsearch.Client bean can be paired with a
	// health.Indicator registered under the same name — and to attach the
	// file:line of this registration to the bean for diagnostics.
	_, file, line, _ := runtime.Caller(0)
	gs.Module(gs.OnProperty("spring.elasticsearch"), func(r gs.BeanProvider, p flatten.Storage) error {
		var m map[string]Config
		if err := conf.Bind(p, &m, "${spring.elasticsearch}"); err != nil {
			return err
		}
		for name, c := range m {
			// The wrapper bean owns the resilience executor + discovery watch, so
			// ApplyResilience arms it (InitMethod) and Close tears it down (Destroy).
			b := r.Provide(newClient,
				gs.IndexArg(1, gs.ValueArg(c)),
			).Name(name).InitMethod("ApplyResilience").Destroy((*ObservedElasticClient).Close)
			b.SetFileLine(file, line)
			// Contribute a health indicator for this instance, injecting the
			// client just registered above by name. The wrapper is what is
			// autowired; the embedded *elasticsearch.Client is handed to the
			// indicator.
			h := r.Provide(func(w *ObservedElasticClient) health.Indicator {
				return health2.NewClientHealth(name, w.Client)
			}, gs.TagArg(name)).Name(name).Export(gs.As[health.Indicator]())
			h.SetFileLine(file, line)
		}
		return nil
	})
}

var starterTag = log.RegisterInfraTag("elasticsearch", "")

// newClient creates a new Elasticsearch client based on the provided
// configuration. The cluster is probed once at startup so that misconfiguration
// or an unreachable cluster fails fast rather than on first use.
//
// When c.ServiceName is set and mesh mode is off, a Resolver is built against
// the registered discovery backend (c.Discovery), its current endpoint snapshot
// is turned into "scheme://host:port" node addresses, and those override
// c.Addresses. Because the elasticsearch client exposes no dialer injection
// point, this is a one-shot resolution at startup — the Resolver is kept alive
// only to keep the lifecycle uniform with the other client starters and is
// stopped on shutdown. In mesh mode the sidecar owns discovery+LB, so the static
// Addresses (or CloudID) are used unchanged. See Config.ServiceName.
func newClient(ctx *gs.ContextProvider, c Config) (*ObservedElasticClient, error) {
	var resolver *discovery.Resolver
	if c.ServiceName != "" && !mesh.Enabled() {
		addrs, r, err := resolveAddresses(ctx.Context, c)
		if err != nil {
			return nil, err
		}
		resolver = r
		c.Addresses = addrs
	}

	d, ok := driverRegistry[c.Driver]
	if !ok {
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(nil, "elasticsearch driver not found: %s", c.Driver)
	}
	client, err := d.CreateClient(ctx.Context, c)
	if err != nil {
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(err, "failed to create elasticsearch client")
	}
	w := &ObservedElasticClient{Client: client, cfg: c, resolver: resolver}
	// The DefaultDriver attaches a dynamic transport (its executor swapped in by
	// ApplyResilience); pick it up so the wrapper can arm it. Custom drivers may
	// not install one - resilience is then simply unavailable for that client.
	if v, ok := dynamicTransports.LoadAndDelete(client); ok {
		w.dyn = v.(*dynamicTransport)
	}
	if err := HealthCheck(ctx.Context, w); err != nil {
		_ = client.Close(context.Background())
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(err, "failed to reach elasticsearch cluster")
	}
	return w, nil
}

// resolveAddresses builds a discovery Resolver for c.ServiceName and returns the
// current endpoint snapshot as "scheme://host:port" node addresses together with
// the Resolver (so the caller can stop its background watch on shutdown). It
// fails fast when no backend is registered or the service has no endpoints.
func resolveAddresses(ctx context.Context, c Config) ([]string, *discovery.Resolver, error) {
	backend, err := discovery.GetDiscovery(c.Discovery)
	if err != nil {
		return nil, nil, err
	}
	r, err := discovery.NewResolver(ctx, backend, c.ServiceName, discovery.WithScheme(c.Scheme))
	if err != nil {
		return nil, nil, errutil.Explain(err, "elasticsearch: resolve service %s", c.ServiceName)
	}
	eps := r.Endpoints()
	if len(eps) == 0 {
		_ = r.Stop()
		return nil, nil, errutil.Explain(nil, "elasticsearch: discovery %q returned no endpoints for %q", c.Discovery, c.ServiceName)
	}
	addrs := make([]string, 0, len(eps))
	for _, ep := range eps {
		addrs = append(addrs, fmt.Sprintf("%s://%s", c.DiscoveryScheme, ep.Addr))
	}
	return addrs, r, nil
}

// HealthCheck reports whether the Elasticsearch cluster is reachable by issuing
// an Info request. It is a thin readiness probe suitable for wiring into a
// health endpoint. A context is always passed to Info because the transport's
// OpenTelemetry instrumentation derives its span from it and panics on a nil
// parent context.
func HealthCheck(ctx context.Context, client *ObservedElasticClient) error {
	res, err := client.Info(client.Info.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		return fmt.Errorf("elasticsearch: info returned %s", res.Status())
	}
	return nil
}
