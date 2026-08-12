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

package StarterGoRedis

import (
	"context"
	"io"

	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/mesh"
)

// closerFunc is an io.Closer backed by a function, so a Driver can hand back
// the teardown for whatever it built (e.g. stopping a discovery resolver watch)
// without the starter keeping a client->resolver side-channel registry. A nil
// closerFunc is a valid no-op Close.
type closerFunc func() error

func (f closerFunc) Close() error {
	if f == nil {
		return nil
	}
	return f()
}

// NopCloser returns an io.Closer whose Close is a no-op, for custom Drivers
// whose client needs no extra teardown. The bundled DefaultDriver returns one of
// these when no discovery resolver is involved (cluster/sentinel/plain Addr).
func NopCloser() io.Closer { return closerFunc(nil) }

// newLiveResolver resolves the registered discovery backend for c and returns a
// Resolver that keeps the service's endpoint set fresh via a background watch.
// It returns (nil, nil) when service-name is unset or mesh mode is enabled (a
// sidecar owns discovery+LB), in which case the caller dials the configured Addr
// directly. The caller owns the lifecycle and stops the resolver via the
// io.Closer returned from CreateClient.
func newLiveResolver(ctx context.Context, c Config) (*discovery.Resolver, error) {
	if c.ServiceName == "" || mesh.Enabled() {
		return nil, nil
	}
	d, err := discovery.GetDiscovery(c.Discovery)
	if err != nil {
		return nil, err
	}
	return discovery.NewResolver(ctx, d, c.ServiceName, discovery.WithScheme(c.Scheme))
}
