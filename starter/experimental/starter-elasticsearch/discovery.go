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

	"go-spring.org/cloud/discovery"
	"go-spring.org/stdlib/errutil"
)

// resolveAddresses builds a discovery Resolver for c.ServiceName and returns the
// current endpoint snapshot as "scheme://host:port" node addresses together with
// the Resolver (so the caller can stop its background watch on shutdown). It
// fails fast when no backend is registered or the service has no endpoints. It
// must only be called when service discovery is in effect (the caller has already
// gated on service-name being set and mesh mode being off).
func resolveAddresses(ctx context.Context, c Config) ([]string, *discovery.Resolver, error) {
	r, err := discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
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

// stopLiveResolver stops the discovery watch behind a client. It is the
// Close-half of the discovery lifecycle, symmetric with resolveAddresses; it is a
// no-op for clients that never had a resolver.
func stopLiveResolver(r *discovery.Resolver) {
	if r != nil {
		_ = r.Stop()
	}
}
