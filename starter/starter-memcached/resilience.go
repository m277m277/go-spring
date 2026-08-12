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
	"go-spring.org/cloud/resilience"
)

// guard runs fn under the resilience executor (when armed), treating
// memcache.ErrCacheMiss as success so a cache miss never trips the breaker, and
// surfacing protection rejections (rate-limited / circuit-open / bulkhead-full)
// to the caller. When exec is nil (resilience disabled) fn runs directly with no
// overhead. It is the single resilience body every Client method routes
// through, so the logic lives in one place rather than being duplicated across
// the 17 wrapped operations.
func guard[T any](exec resilience.Executor, resource string, fn func() (T, error)) (T, error) {
	var zero T
	if exec == nil {
		return fn()
	}
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
		// A real op error propagated through the executor; callErr holds it.
		return result, callErr
	}
	return result, callErr
}

// guardErr is the error-only variant of [guard], for operations that return no
// payload (Set/Delete/Ping/...). It shares the same resilience + ErrCacheMiss
// semantics; the two-variant split keeps the call sites typed rather than
// routing through any + runtime assertion.
func guardErr(exec resilience.Executor, resource string, fn func() error) error {
	if exec == nil {
		return fn()
	}
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
		return callErr
	}
	return callErr
}
