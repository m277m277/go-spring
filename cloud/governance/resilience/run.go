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

package resilience

import (
	"context"
	"errors"
)

// IsRejection reports whether err is one of the resilience protection rejects:
// rate-limited, circuit-open, or bulkhead-full. Every client adapter checks
// these three sentinels after an Execute to decide whether a rejection should be
// surfaced verbatim (rather than treated as the operation's own error), so the
// check is shared here rather than copy-pasted per starter.
func IsRejection(err error) bool {
	return errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrCircuitOpen) ||
		errors.Is(err, ErrBulkheadFull)
}

// Run executes call under exec, scoping rate-limiter/breaker state to resource.
// It is the single "run one client operation through the resilience executor"
// body that every client adapter (redis.Hook, gorm callback, connection wrapper,
// http.RoundTripper, ...) otherwise copy-pastes, extracted so the nil-as-success
// and rejection/fault translation semantics live in exactly one place.
//
// nilErr classifies a returned error as "not a fault for resilience purposes":
// a cache miss / "no rows" is a normal outcome, so it must neither trip the
// breaker nor trigger a retry. Pass nil for clients with no such sentinel.
// Examples: redis.Nil, gorm.ErrRecordNotFound, memcache.ErrCacheMiss.
//
// The return is the operation's own value (a command reply, a fetched item, ...)
// plus the error to propagate. Callers whose operation yields nothing pass
// struct{}{} for T and discard the result. Semantics:
//
//   - A resilience rejection ([ErrRateLimited] / [ErrCircuitOpen] /
//     [ErrBulkheadFull]) is returned verbatim.
//   - On a normal downstream failure, the returned error is call's own error.
//   - When a fault injector short-circuited the attempt before call ran (call's
//     error is nil but the executor still returns an injected error), the
//     injected error is preferred so it is not silently swallowed as success.
func Run[T any](ctx context.Context, exec Executor, resource string,
	nilErr func(error) bool, call func(context.Context) (T, error)) (T, error) {

	var result T
	if exec == nil {
		return call(ctx)
	}
	var callErr error
	execErr := exec.Execute(ctx, resource, func(attemptCtx context.Context) error {
		result, callErr = call(attemptCtx)
		if callErr != nil && !(nilErr != nil && nilErr(callErr)) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or a tolerated sentinel (cache miss / "no rows")
	})
	if execErr != nil {
		if IsRejection(execErr) {
			return result, execErr // rejected before (or around) the call
		}
		// On the normal failure path execErr equals callErr (the closure returned
		// it). They diverge only when the closure never ran — e.g. a fault injector
		// short-circuited the attempt before reaching call, leaving callErr nil
		// while the executor still returns the injected error. Prefer execErr so
		// the failure is not silently swallowed as success.
		if callErr == nil {
			return result, execErr
		}
		return result, callErr
	}
	return result, callErr
}
