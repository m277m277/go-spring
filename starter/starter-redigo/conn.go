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

package StarterRedigo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
)

// Conn wraps a redis.Conn so every command flows through the shared observe kit
// (trace span + duration/in-flight metric + access log) and, when resilience is
// armed, through the resilience executor.
//
// It implements the optional DoContext / DoWithTimeout interfaces so the wrapper
// is transparent to type-asserting helpers (redis.DoContext etc.): when the
// inner conn supports them the call is instrumented with the caller's context
// (linking the client span to the request trace); otherwise it falls back to the
// plain Do path.
//
// The executor sits INSIDE the span (span start → executor → inner call → span
// end), so one Execute covers any retries the policy drives. redis.ErrNil (a
// cache miss) is treated as success so it never trips the breaker — the redigo
// analog of gorm.ErrRecordNotFound; protection rejections (rate-limited /
// circuit-open / bulkhead-full) surface to the caller verbatim.
//
// Do and DoWithTimeout carry no context, so their spans are roots and an
// attempt-timeout cannot interrupt them — prefer DoContext when either matters.
// Send / Flush / Receive (pipelining) are left to the embedded Conn
// uninstrumented; correlating queued Sends with their Receives needs deeper,
// out-of-scope instrumentation.
type Conn struct {
	redis.Conn
	obs         *observe.Observer
	exec        resilience.Executor // nil when resilience is disabled
	resource    string              // resilience resource label (stable per pool)
	interceptor CommandInterceptor  // nil when no per-command hook is registered
}

// Do is the common, context-less path: its span is a root and an attempt-timeout
// cannot interrupt it. It is mandatory (part of the redis.Conn interface) and the
// fallback for the optional DoContext / DoWithTimeout when the inner conn lacks them.
func (c *Conn) Do(cmd string, args ...interface{}) (interface{}, error) {
	return c.run(context.Background(), cmd, args, func(context.Context) (interface{}, error) {
		return c.Conn.Do(cmd, args...)
	})
}

// DoContext instruments the context-aware path; its span links to the caller and
// an attempt-timeout can interrupt it. Falls back to Do when the inner conn has
// no DoContext.
func (c *Conn) DoContext(ctx context.Context, cmd string, args ...interface{}) (interface{}, error) {
	type ctxDoer interface {
		DoContext(context.Context, string, ...interface{}) (interface{}, error)
	}
	inner, ok := c.Conn.(ctxDoer)
	if !ok {
		return c.Do(cmd, args...)
	}
	return c.run(ctx, cmd, args, func(actx context.Context) (interface{}, error) {
		return inner.DoContext(actx, cmd, args...)
	})
}

// DoWithTimeout instruments the timeout variant; its span is a root (same as Do)
// but the redigo read-timeout still bounds the call. Falls back to Do when the
// inner conn has no DoWithTimeout.
func (c *Conn) DoWithTimeout(timeout time.Duration, cmd string, args ...interface{}) (interface{}, error) {
	type timeoutDoer interface {
		DoWithTimeout(time.Duration, string, ...interface{}) (interface{}, error)
	}
	inner, ok := c.Conn.(timeoutDoer)
	if !ok {
		return c.Do(cmd, args...)
	}
	return c.run(context.Background(), cmd, args, func(context.Context) (interface{}, error) {
		return inner.DoWithTimeout(timeout, cmd, args...)
	})
}

// run dispatches one command. When a per-command interceptor is registered it
// runs OUTERMOST — before the observe span and before the resilience executor —
// so it can short-circuit (e.g. a local-cache hit) without starting a span or
// consuming a breaker permit. next enters the built-in path (builtinRun); call
// it exactly once to reach Redis, or skip it to short-circuit. With no
// interceptor registered run delegates straight to builtinRun at zero extra
// cost.
func (c *Conn) run(ctx context.Context, cmd string, args []interface{},
	call func(context.Context) (interface{}, error)) (reply interface{}, err error) {

	if c.interceptor != nil {
		return c.interceptor(ctx, cmd, args, func(actx context.Context) (interface{}, error) {
			return c.builtinRun(actx, cmd, args, call)
		})
	}
	return c.builtinRun(ctx, cmd, args, call)
}

// builtinRun is the built-in command path: start a client span for cmd (when
// observe is enabled), run call under the resilience executor (when armed), and
// end the span with the result. ctx is the span parent — the caller's context
// for DoContext (so the span links to the request trace and an attempt-timeout
// can interrupt it), background for the context-less paths. The executor derives
// its per-attempt context from ctx; the inner call receives that attempt context,
// which only DoContext threads through (the others ignore it — redigo's Do and
// DoWithTimeout take no context).
//
// The executor sits INSIDE the span (span start → executor → inner call → span
// end), so one Execute covers any retries the policy drives. redis.ErrNil (a
// cache miss) is mapped to success so it never trips the breaker; protection
// rejections (rate-limited / circuit-open / bulkhead-full) surface verbatim.
// When observe is disabled (obs == nil) no span is started; when resilience is
// disabled (exec == nil) call runs directly — so with both off (and no
// interceptor) the Conn wrapper is a near-zero-cost pass-through that still
// keeps the DoContext / DoWithTimeout interfaces transparent.
func (c *Conn) builtinRun(ctx context.Context, cmd string, args []interface{},
	call func(context.Context) (interface{}, error)) (reply interface{}, err error) {

	var sp *observe.Span
	if c.obs != nil {
		ctx, sp = c.obs.Start(ctx, cmd, summarizeCommand(cmd, args))
	}
	defer func() {
		if sp != nil {
			sp.End(err)
		}
	}()

	if c.exec == nil {
		return call(ctx) // resilience disabled — no executor overhead
	}

	var callErr error
	execErr := c.exec.Execute(ctx, c.resource, func(attemptCtx context.Context) error {
		reply, callErr = call(attemptCtx)
		if callErr != nil && !errors.Is(callErr, redis.ErrNil) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or cache miss
	})

	if errors.Is(execErr, resilience.ErrRateLimited) ||
		errors.Is(execErr, resilience.ErrCircuitOpen) ||
		errors.Is(execErr, resilience.ErrBulkheadFull) {
		return nil, execErr // rejected before (or around) the command
	}

	// On success execErr is nil and callErr is nil. On a command failure the fn
	// closure returned callErr, so execErr == callErr and either is correct.
	// They diverge only when fn never ran — e.g. a fault injector (cloud/fault)
	// short-circuited the attempt before reaching the command — leaving callErr
	// nil while the executor still returns the injected error. Prefer execErr so
	// such failures surface instead of being silently swallowed as success.
	if execErr != nil {
		return reply, execErr
	}
	return reply, callErr
}

// summarizeCommand renders a short, loggable summary of the command — the
// command name plus the first argument (typically the key) — bounded by the
// Observer's ObserveConfig.MaxArgBytes. The full argument list is intentionally
// not logged: keys are enough to locate an op, and values may be sensitive or
// large.
func summarizeCommand(cmd string, args []interface{}) string {
	if len(args) == 0 {
		return cmd
	}
	return fmt.Sprintf("%s %v", cmd, args[0])
}
