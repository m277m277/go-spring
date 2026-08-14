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

// driver.go is the "construction seam" concept of this starter: the Driver
// interface + registry + DefaultDriver, which owns full client assembly
// (including service-discovery resolution). It mirrors starter-redigo's
// driver.go.
package StarterMemcached

import (
	"context"
	"sync"

	"github.com/bradfitz/gomemcache/memcache"
	"go-spring.org/cloud/discovery"
	"go-spring.org/stdlib/errutil"
)

var driverRegistry = map[string]Driver{}

func init() {
	RegisterDriver("DefaultDriver", DefaultDriver{})
}

// Driver interface defines how to create a Memcached client.
type Driver interface {
	CreateClient(ctx context.Context, c Config) (*memcache.Client, error)
}

// RegisterDriver registers a Memcached driver with the given name.
// It panics if the driver name has already been registered.
func RegisterDriver(name string, driver Driver) {
	if _, ok := driverRegistry[name]; ok {
		panic("memcached driver already registered: " + name)
	}
	driverRegistry[name] = driver
}

// DefaultDriver is the default implementation of the Driver interface.
type DefaultDriver struct{}

// CreateClient creates a new Memcached client based on the provided configuration.
//
// When c.ServiceName is set (and mesh mode is not enabled), the server list is
// resolved through the registered discovery backend (c.Discovery) instead of
// using c.Servers. A discovery.Resolver seeds the snapshot with an explicit
// Resolve and keeps it fresh via a background Watch; gomemcache hashes keys
// onto a fixed server set chosen at client creation, so only the initial
// snapshot is applied to the client — the Resolver is retained solely to own
// the watch lifecycle (Stop on shutdown).
//
// In mesh mode (mesh.Enabled) discovery is skipped entirely: a sidecar owns
// discovery+LB, so the client connects straight to the configured static
// Servers list (the service's stable DNS address).
func (DefaultDriver) CreateClient(ctx context.Context, c Config) (*memcache.Client, error) {
	servers := c.Servers
	resolver, err := newLiveResolver(ctx, c)
	if err != nil {
		return nil, errutil.Explain(err, "memcached: discovery resolve %q failed", c.ServiceName)
	}
	if resolver != nil {
		eps := resolver.Endpoints()
		if len(eps) == 0 {
			_ = resolver.Stop()
			return nil, errutil.Explain(nil, "memcached: discovery returned no endpoints for %q", c.ServiceName)
		}
		servers = make([]string, 0, len(eps))
		for _, ep := range eps {
			servers = append(servers, ep.Addr)
		}
	}
	client := memcache.New(servers...)
	if c.Timeout > 0 {
		client.Timeout = c.Timeout
	}
	if c.MaxIdleConns > 0 {
		client.MaxIdleConns = c.MaxIdleConns
	}
	if resolver != nil {
		resolvers.Store(client, resolver)
	}
	return client, nil
}

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
	return discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
}

// stopLiveResolver stops the discovery watch behind the given client value. It is
// the Close-half of the discovery lifecycle, symmetric with newLiveResolver; it
// is a no-op for clients that never had a resolver.
func stopLiveResolver(client any) {
	if v, ok := resolvers.LoadAndDelete(client); ok {
		_ = v.(*discovery.Resolver).Stop()
	}
}
