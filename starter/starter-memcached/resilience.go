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
	"errors"

	"github.com/bradfitz/gomemcache/memcache"
	"go-spring.org/cloud/governance/resilience"
)

// guard runs fn under the resilience executor, treating
// memcache.ErrCacheMiss as success so a cache miss never trips the breaker, and
// surfacing protection rejections (rate-limited / circuit-open / bulkhead-full)
// to the caller. When governance is off the resolved executor is a no-op, so fn
// runs with a single function-call overhead. It is the single resilience body
// every Client method routes through, so the logic lives in one place rather
// than being duplicated across the 17 wrapped operations.
func guard[T any](exec resilience.Executor, resource string, fn func() (T, error)) (T, error) {
	var zero T
	var result T
	var callErr error
	execErr := exec.Execute(context.Background(), resource, func(context.Context) error {
		result, callErr = fn()
		if callErr != nil && !errors.Is(callErr, memcache.ErrCacheMiss) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or cache miss
	})
	if execErr != nil {
		if errors.Is(execErr, resilience.ErrRateLimited) ||
			errors.Is(execErr, resilience.ErrCircuitOpen) ||
			errors.Is(execErr, resilience.ErrBulkheadFull) {
			// Rejected before (or around) the op: surface the rejection.
			return zero, execErr
		}
		// On the normal failure path execErr equals callErr. They diverge only
		// when the closure body never ran — e.g. a fault injector short-circuited
		// the attempt before reaching fn — leaving callErr nil. Prefer execErr so
		// the failure is not swallowed as success.
		if callErr == nil {
			return zero, execErr
		}
		return result, callErr
	}
	return result, callErr
}

// guardErr is the error-only variant of [guard], for operations that return no
// payload (Set/Delete/Ping/...). It shares the same resilience + ErrCacheMiss
// semantics; the two-variant split keeps the call sites typed rather than
// routing through any + runtime assertion.
func guardErr(exec resilience.Executor, resource string, fn func() error) error {
	var callErr error
	execErr := exec.Execute(context.Background(), resource, func(context.Context) error {
		callErr = fn()
		if callErr != nil && !errors.Is(callErr, memcache.ErrCacheMiss) {
			return callErr
		}
		return nil
	})
	if execErr != nil {
		if errors.Is(execErr, resilience.ErrRateLimited) ||
			errors.Is(execErr, resilience.ErrCircuitOpen) ||
			errors.Is(execErr, resilience.ErrBulkheadFull) {
			return execErr
		}
		// Prefer execErr when the closure never ran (fault injected before fn);
		// see guard for the rationale.
		if callErr == nil {
			return execErr
		}
		return callErr
	}
	return callErr
}
