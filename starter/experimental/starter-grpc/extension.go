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

package StarterGrpc

import (
	"sync"

	"google.golang.org/grpc"
)

// Extension state holds user-registered interceptors that compose onto the
// built-in server chain (LoadTest -> Tracing -> Metrics -> Resilience). It is
// written during process init / before the container builds the server and read
// once when buildOptions assembles the chain. A mutex guards it so registration
// is safe regardless of timing; registration is rare, so the lock is free.
//
// Before this seam existed, adding a custom server interceptor required
// replacing the entire *grpc.Server (the starter owned the only
// grpc.NewServer call). UseUnaryInterceptor / UseStreamInterceptor let an
// application compose its own interceptors onto the built-in stack instead.
var (
	extMu      sync.RWMutex
	userUnary  []grpc.UnaryServerInterceptor
	userStream []grpc.StreamServerInterceptor
)

// UseUnaryInterceptor prepends a unary server interceptor to the built-in
// chain. Registered interceptors run OUTERMOST (first-registered = outermost),
// mirroring starter-gin's EngineMiddleware model: a user guard (auth, request
// filtering) sees the request before the built-in tracing/metrics/resilience
// layers, and can short-circuit before any work is observed. Call from an init
// function (or otherwise before the container builds the server). Multiple
// calls compose.
func UseUnaryInterceptor(i grpc.UnaryServerInterceptor) {
	if i == nil {
		return
	}
	extMu.Lock()
	defer extMu.Unlock()
	userUnary = append(userUnary, i)
}

// UseStreamInterceptor is the streaming-RPC counterpart of UseUnaryInterceptor.
func UseStreamInterceptor(i grpc.StreamServerInterceptor) {
	if i == nil {
		return
	}
	extMu.Lock()
	defer extMu.Unlock()
	userStream = append(userStream, i)
}

// currentUserUnary returns a snapshot of registered unary interceptors in
// registration order (first-registered first).
func currentUserUnary() []grpc.UnaryServerInterceptor {
	extMu.RLock()
	defer extMu.RUnlock()
	return append([]grpc.UnaryServerInterceptor(nil), userUnary...)
}

// currentUserStream returns a snapshot of registered stream interceptors.
func currentUserStream() []grpc.StreamServerInterceptor {
	extMu.RLock()
	defer extMu.RUnlock()
	return append([]grpc.StreamServerInterceptor(nil), userStream...)
}
