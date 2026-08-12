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

package StarterGormClickhouse

import (
	"context"
	"sync"

	"go-spring.org/cloud/discovery"
	"gorm.io/gorm"
)

// liveDialers tracks the discovery-backed resolver behind each client, so the
// wrapper's Close can stop the watch when the client is torn down.
var liveDialers sync.Map // *gorm.DB -> *discovery.Resolver

// newLiveResolver resolves the registered discovery backend for c and returns a
// Resolver that keeps the service's endpoint set fresh via a background watch. It
// returns (nil, nil) when service-name is unset or mesh mode is enabled (a sidecar
// owns discovery+LB), in which case the caller dials the configured Addr directly.
// The caller owns the lifecycle and must release the resolver via stopLiveResolver.
func newLiveResolver(ctx context.Context, c Config) (*discovery.Resolver, error) {
	return discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
}

// stopLiveResolver stops the discovery watch behind the given client value. It is
// the Close-half of the discovery lifecycle, symmetric with newLiveResolver; it
// is a no-op for clients that never had a resolver.
func stopLiveResolver(db *gorm.DB) {
	if v, ok := liveDialers.LoadAndDelete(db); ok {
		_ = v.(*discovery.Resolver).Stop()
	}
}
