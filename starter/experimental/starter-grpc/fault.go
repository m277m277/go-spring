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
	"context"

	"go-spring.org/cloud/governance/fault"
	"google.golang.org/grpc"
)

// FaultUnaryInterceptor injects faults into inbound unary RPCs: per the
// injector's rules a request is made to fail (injected error) or slow down
// (configured latency) before the handler runs. It is the server-side
// counterpart to the client starters' fault.WrapExecutor — letting an operator
// "set fire" to a running gRPC server to verify its observe and upstream
// clients' retry/breaker behavior. Installed innermost (after tracing/metrics/
// resilience) so the injected error flows back through those layers and is
// observed. The injector is resolved from the neutral [fault.InjectorFor] seam
// (backed by the governance center) on each call; when no injector is registered
// fault.Apply is a transparent pass-through, so this is always installed.
func FaultUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var resp any
		err := fault.Apply(ctx, fault.InjectorFor(), "grpc:"+info.FullMethod, func() error {
			var e error
			resp, e = handler(ctx, req)
			return e
		})
		return resp, err
	}
}

// FaultStreamInterceptor is the streaming-RPC counterpart of
// FaultUnaryInterceptor. Latency is applied before the stream handler runs; an
// injected error aborts it.
func FaultStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return fault.Apply(ss.Context(), fault.InjectorFor(), "grpc:"+info.FullMethod, func() error {
			return handler(srv, ss)
		})
	}
}
