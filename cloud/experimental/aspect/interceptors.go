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

package aspect

import (
	"fmt"
	"slices"
	"time"
)

// Recover returns an interceptor that turns a panic raised anywhere further down
// the chain (including the target) into an error, so a single misbehaving
// operation cannot crash the process. The recovered value is wrapped with %v; if
// it already is an error it is preserved via errors.Is/As through %w.
func Recover() Interceptor {
	return InterceptorFunc(func(jp *Joinpoint) (result any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					err = fmt.Errorf("aspect: recovered panic in %q: %w", jp.Method, e)
				} else {
					err = fmt.Errorf("aspect: recovered panic in %q: %v", jp.Method, r)
				}
				result = nil
			}
		}()
		return jp.Proceed(jp.Context)
	})
}

// Timing returns an interceptor that measures how long the remaining chain takes
// and reports it through report, together with the method name and final error.
// It is the seam for method-level metrics and audit logging (the埋点/审计
// equivalent). report is called even when the operation fails so failures are
// observable; it must be safe for concurrent use.
func Timing(report func(method string, d time.Duration, err error)) Interceptor {
	return InterceptorFunc(func(jp *Joinpoint) (any, error) {
		start := time.Now()
		v, err := jp.Proceed(jp.Context)
		if report != nil {
			report(jp.Method, time.Since(start), err)
		}
		return v, err
	})
}

// Only returns an interceptor that applies inner only when the joinpoint's method
// is one of methods; for any other method it proceeds straight through. It is the
// pointcut equivalent: a way to scope a concern to a subset of the operations a
// chain guards without building a separate chain per method.
func Only(inner Interceptor, methods ...string) Interceptor {
	return InterceptorFunc(func(jp *Joinpoint) (any, error) {
		if slices.Contains(methods, jp.Method) {
			return inner.Intercept(jp)
		}
		return jp.Proceed(jp.Context)
	})
}
