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

	"go-spring.org/cloud/resilience"
	"go-spring.org/observe/resilience"
	"google.golang.org/grpc"
)

// resilienceInterceptors are the unary/stream wrappers built from the server's
// resilience policy, if any.
type resilienceInterceptors struct {
	unary grpc.UnaryServerInterceptor
}

// buildResilienceInterceptors constructs an inbound-admission executor via the
// NEUTRAL provider seam [resilience.ExecutorFor] and returns the unary
// interceptor that runs each RPC through it. starter-govern registers a provider
// backed by the governance center, so this server gets its rate-limit /
// bulkhead / breaker policy WITHOUT injecting *govern.Center or even importing
// cloud/govern. When governance is not configured the seam yields a transparent
// no-op executor (fn runs once, untouched). Hot-reload is driven on the backing
// executor by the provider. The executor is wrapped with observe-resilience so
// breaker trips / rate rejects / bulkhead rejects emit span + counter +
// histogram + access log.
//
// Inbound resilience is admission control: set RateLimit / MaxConcurrent to
// protect the server from overload; ErrRateLimited / ErrBulkheadFull /
// ErrCircuitOpen surface as the executor's error on the RPC. Do NOT configure
// retry for inbound (a handler that has already produced side effects cannot be
// replayed) — leave MaxRetries at 0: inbound serving is not idempotent.
func (s *SimpleGrpcServer) buildResilienceInterceptors() (resilienceInterceptors, bool) {
	resource := resilience.ResourceLabel("grpc", s.cfg.Addr)
	exec := resilience.ExecutorFor(resource)
	exec = resilobserve.WrapExecutor(exec, "grpc", s.cfg.Observability)
	return resilienceInterceptors{
		unary: resilienceUnaryInterceptor(exec, resource),
	}, true
}

// resilienceUnaryInterceptor runs each unary RPC through the executor so the
// configured rate-limit / bulkhead / breaker (admission) policy is enforced
// before the handler runs.
func resilienceUnaryInterceptor(exec resilience.Executor, resource string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var resp any
		err := exec.Execute(ctx, resource, func(ctx context.Context) error {
			var e error
			resp, e = handler(ctx, req)
			return e
		})
		return resp, err
	}
}
