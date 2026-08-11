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

package StarterGin

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go-spring.org/observe/resilience"
	"go-spring.org/cloud/experimental/resilience"
)

// buildAdmission builds the inbound admission middleware from cfg, or returns
// (nil, nil) when resilience is disabled. The executor is wrapped with
// observe-resilience so breaker trips / rejects emit span + counter + histogram
// + access log.
func buildAdmission(cfg Config) (gin.HandlerFunc, error) {
	if !cfg.Resilience.Enabled {
		return nil, nil
	}
	drv, err := resilience.MustGetDriver(cfg.Resilience.Driver)
	if err != nil {
		return nil, err
	}
	exec, err := drv.NewExecutor(cfg.Resilience.Policy())
	if err != nil {
		return nil, err
	}
	exec = resilobserve.WrapExecutor(exec, "gin", cfg.Observability)
	return resilienceAdmission(exec, resilience.ResourceLabel("gin", cfg.Address)), nil
}

// resilienceAdmission is the inbound admission middleware: each request runs
// through exec so the configured rate-limit / bulkhead / breaker policy is
// enforced before the handler chain. Rejects map to 429 (rate/bulkhead) or 503
// (circuit open); a handler-emitted 5xx counts as a failure for the breaker.
//
// Inbound admission must NOT retry — a handler that has already produced side
// effects cannot be replayed (matching resilience.NewHandler). Leave
// Policy.MaxRetries at 0; a Written() guard also prevents reentry regardless.
func resilienceAdmission(exec resilience.Executor, resource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var served bool
		err := exec.Execute(c.Request.Context(), resource, func(ctx context.Context) error {
			if served {
				return nil // reentry guard: handler already ran this request
			}
			c.Next()
			served = c.Writer.Written()
			if c.Writer.Status() >= 500 {
				return errHTTP5xx{code: c.Writer.Status()}
			}
			return nil
		})
		if err == nil {
			return
		}
		switch {
		case errors.Is(err, resilience.ErrRateLimited), errors.Is(err, resilience.ErrBulkheadFull):
			if !c.Writer.Written() {
				c.AbortWithStatus(http.StatusTooManyRequests)
			}
		case errors.Is(err, resilience.ErrCircuitOpen):
			if !c.Writer.Written() {
				c.AbortWithStatus(http.StatusServiceUnavailable)
			}
		}
	}
}

// errHTTP5xx is the failure signal a handler-emitted 5xx feeds back into the
// breaker (the breaker counts non-nil errors from fn).
type errHTTP5xx struct{ code int }

func (e errHTTP5xx) Error() string { return fmt.Sprintf("http: server returned %d", e.code) }
