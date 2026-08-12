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
	"net/url"

	"go-spring.org/cloud/discovery"
	"go-spring.org/stdlib/errutil"
)

// resolveURI resolves c.ServiceName through the registered discovery backend,
// picks one live endpoint, and rewrites the URI's host to that address. It
// returns the Resolver alongside so the caller can keep the watch alive and stop
// it on shutdown. It must only be called when service discovery is in effect (the
// caller has already gated on service-name being set and mesh mode being off).
func resolveURI(ctx context.Context, c Config) (string, *discovery.Resolver, error) {
	r, err := discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
	if err != nil {
		return "", nil, errutil.Explain(err, "neo4j: resolve service %s", c.ServiceName)
	}
	ep, err := r.Pick()
	if err != nil {
		_ = r.Stop()
		return "", nil, errutil.Explain(err, "neo4j: pick endpoint for %s", c.ServiceName)
	}
	u, err := url.Parse(c.URI)
	if err != nil {
		_ = r.Stop()
		return "", nil, errutil.Explain(err, "neo4j: parse uri %s", c.URI)
	}
	u.Host = ep.Addr
	return u.String(), r, nil
}

// stopLiveResolver stops the discovery watch behind a client. It is the
// Close-half of the discovery lifecycle, symmetric with resolveURI; it is a no-op
// for clients that never had a resolver.
func stopLiveResolver(r *discovery.Resolver) {
	if r != nil {
		_ = r.Stop()
	}
}
