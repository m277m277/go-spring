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

	"go-spring.org/observe-resilience"
	"go-spring.org/spring/experimental/cloud/resilience"
	"google.golang.org/grpc"
)

// resilienceInterceptors are the unary/stream wrappers built from the server's
// resilience policy, if any.
type resilienceInterceptors struct {
	unary grpc.UnaryServerInterceptor
}

// buildResilienceInterceptors constructs an inbound-admission executor from the
// configured policy and returns the unary interceptor that runs each RPC through
// it. The executor is wrapped with observe-resilience so breaker trips / rate
// rejects / bulkhead rejects emit span + counter + histogram + access log.
//
// Inbound resilience is admission control: set RateLimit / MaxConcurrent to
// protect the server from overload; ErrRateLimited / ErrBulkheadFull /
// ErrCircuitOpen surface as the executor's error on the RPC. Do NOT configure
// retry for inbound (a handler that has already produced side effects cannot be
// replayed) — leave MaxRetries at 0, matching resilience.NewHandler's contract.
// Returns ok=false when resilience is disabled, so buildOptions skips it.
func (s *SimpleGrpcServer) buildResilienceInterceptors() (resilienceInterceptors, bool) {
	c := s.cfg
	if !c.Resilience.Enabled {
		return resilienceInterceptors{}, false
	}
	drv, err := resilience.MustGetDriver(c.Resilience.Driver)
	if err != nil {
		// A missing driver is fatal at boot — same posture as the other starters.
		panic(err)
	}
	exec, err := drv.NewExecutor(c.Resilience.Policy())
	if err != nil {
		panic(err)
	}
	exec = resilobserve.WrapExecutor(exec, "grpc", c.Observability)
	resource := resilience.ResourceLabel("grpc", c.Addr)
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
