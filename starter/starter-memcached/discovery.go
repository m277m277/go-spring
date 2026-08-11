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

package StarterMemcached

import (
	"context"
	"sync"

	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/mesh"
)

// resolvers tracks the discovery-backed Resolver behind each client built by
// DefaultDriver, so Close can stop the background watch on shutdown. gomemcache
// shards keys across a static server set chosen at client creation, so the live
// endpoint updates from the watch are NOT re-applied to the client mid-flight —
// the Resolver is kept only to own the watch lifecycle and to provide a Stop
// hook. A changing cluster membership requires a restart.
var resolvers sync.Map // *memcache.Client -> *discovery.Resolver

// newLiveResolver resolves the registered discovery backend for c and returns a
// Resolver that keeps the service's endpoint set fresh via a background watch. It
// returns (nil, nil) when service-name is unset or mesh mode is enabled (a sidecar
// owns discovery+LB), in which case the caller uses the configured Servers list.
// The caller owns the lifecycle and must release the resolver via stopLiveResolver.
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

// stopLiveResolver stops the discovery watch behind the given client value. It is
// the Close-half of the discovery lifecycle, symmetric with newLiveResolver; it
// is a no-op for clients that never had a resolver.
func stopLiveResolver(client any) {
	if v, ok := resolvers.LoadAndDelete(client); ok {
		_ = v.(*discovery.Resolver).Stop()
	}
}
