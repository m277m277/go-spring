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

package fault

import (
	"context"

	"go-spring.org/cloud/resilience"
)

// WrapExecutor returns an Executor that injects faults into fn before
// delegating to inner.
//
// The injection happens INSIDE inner's retry loop: the wrapped fn either
// returns the injected error (so the real executor retries, the breaker counts
// the failure, and observe records the outcome) or calls the real fn. This is
// what makes "setting fire" validate the resilience stack — a fault flows
// through retry/breaker/timeout/Fallback exactly as a real downstream failure
// would, rather than short-circuiting at the boundary where none of those
// mechanisms are in play.
//
// nil inner => returns nil, and nil injector => returns inner unchanged,
// preserving the zero-config transparency invariant mirrored from the
// resilience seams ([resilience.NewDialer], [resilience.NewRoundTripper]).
func WrapExecutor(inner resilience.Executor, in *Injector) resilience.Executor {
	if inner == nil || in == nil {
		return inner
	}
	return &faultExecutor{inner: inner, in: in}
}

type faultExecutor struct {
	inner resilience.Executor
	in    *Injector
}

// Execute wraps fn so each attempt is faulted per the injector's live config
// before the real executor sees it. An injected latency sleeps first (cancellable
// via the attempt context); if the sleep is cancelled the context error is
// returned so the executor's budget/timeout logic reacts rather than retrying
// blindly.
func (e *faultExecutor) Execute(ctx context.Context, resource string, fn func(context.Context) error) error {
	wrapped := func(attemptCtx context.Context) error {
		inject, sleep, injErr := e.in.maybe(resource)
		if sleep > 0 && !resilience.SleepFor(attemptCtx, sleep) {
			// The latency sleep was cancelled (caller cancel or budget expiry);
			// surface the context error so the executor stops retrying.
			if err := attemptCtx.Err(); err != nil {
				return err
			}
		}
		if inject {
			return injErr
		}
		return fn(attemptCtx)
	}
	return e.inner.Execute(ctx, resource, wrapped)
}

// Close releases the inner executor's resources.
func (e *faultExecutor) Close() error { return e.inner.Close() }

// Refresh forwards the new policy to the inner executor. The fault injector
// itself has no policy to refresh — its own config is swapped via
// [Injector.SetConfig] from the starter's gs.Dync binding.
func (e *faultExecutor) Refresh(p resilience.Policy) error { return e.inner.Refresh(p) }
