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

// wrapDial wraps the pool's Dial / DialContext so every connection handed out
// is an obsConn that instruments each command through the shared observe kit
// (trace span + duration/in-flight metric + access log) and, when resilience is
// armed, through the executor. It is called by Init (the gs
// InitMethod) after the observer + executor are built; the obsConn captures them
// at install time, so a nil o.exec (resilience disabled) means the inner conn is
// called directly with no executor overhead.
//
// The kit rides the OTel globals (starter-otel) and the project log, so this is
// a near-zero-cost opt-in that needs no per-component adaptation: when
// starter-otel is absent, trace+metric are no-ops; the access log is gated by
// cfg.Level (default brief).
func (o *Pool) wrapDial() {
	pool := o.Pool
	if pool == nil {
		return
	}
	wrap := func(c redis.Conn) redis.Conn {
		if c == nil {
			return nil
		}
		return &obsConn{Conn: c, obs: o.obs, exec: o.exec, resource: o.resource}
	}
	if pool.Dial != nil {
		d := pool.Dial
		pool.Dial = func() (redis.Conn, error) {
			c, err := d()
			if err != nil {
				return nil, err
			}
			return wrap(c), nil
		}
	}
	if pool.DialContext != nil {
		d := pool.DialContext
		pool.DialContext = func(ctx context.Context) (redis.Conn, error) {
			c, err := d(ctx)
			if err != nil {
				return nil, err
			}
			return wrap(c), nil
		}
	}
}

// obsConn wraps a redis.Conn so every command flows through the observe kit,
// and — when resilience is armed — through the resilience executor as well.
//
// Do is the common path and is fully instrumented. DoContext / DoWithTimeout
// (the optional ConnWithContext / ConnWithTimeout interfaces) are implemented so
// the wrapper is transparent to apps and helpers that type-assert those
// interfaces (redis.DoContext etc.) — when the inner conn supports them, the
// call is instrumented with the caller's context (so the client span links to
// the request trace); when it does not, the call falls back to the plain Do path.
//
// The resilience executor sits INSIDE the observe span (span start → executor →
// inner call → span end), so one Execute covers any retries the policy drives.
// redis.ErrNil (a cache miss / "key not found") is treated as success so it never
// trips the breaker — the redigo analog of gorm.ErrRecordNotFound. Rejections
// (rate-limited / circuit-open / bulkhead-full) surface to the caller verbatim.
//
// Send / Flush / Receive (pipelining) are left to the embedded Conn
// uninstrumented: correlating queued Send calls with the Receive results needs a
// separate, deeper instrumentation and is out of scope here.
//
// redigo's Do carries no context, so its span is a root span (not linked to the
// caller's request trace). Prefer DoContext (via redis.DoContext) when you need
// the client span linked — that path flows ctx through.
type obsConn struct {
	redis.Conn
	obs      *observe.Observer
	exec     resilience.Executor // nil when resilience is disabled
	resource string              // resilience resource label (stable per pool)
}

func (c *obsConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	_, sp := c.obs.Start(context.Background(), cmd, summarizeCommand(cmd, args))
	reply, err := c.run(context.Background(), func(context.Context) (interface{}, error) {
		return c.Conn.Do(cmd, args...)
	})
	sp.End(err)
	return reply, err
}

// DoContext instruments the context-aware path and links the span to the caller.
func (c *obsConn) DoContext(ctx context.Context, cmd string, args ...interface{}) (interface{}, error) {
	type ctxDoer interface {
		DoContext(context.Context, string, ...interface{}) (interface{}, error)
	}
	inner, ok := c.Conn.(ctxDoer)
	if !ok {
		// Inner conn has no DoContext — best-effort fallback to the plain path.
		return c.Do(cmd, args...)
	}
	ctx, sp := c.obs.Start(ctx, cmd, summarizeCommand(cmd, args))
	// Pass the executor's per-attempt context through to the inner DoContext so a
	// policy attempt-timeout can actually interrupt the call.
	reply, err := c.run(ctx, func(actx context.Context) (interface{}, error) {
		return inner.DoContext(actx, cmd, args...)
	})
	sp.End(err)
	return reply, err
}

// DoWithTimeout instruments the timeout variant. redigo's ConnWithTimeout has no
// context, so the span is a root span (same caveat as Do).
func (c *obsConn) DoWithTimeout(timeout time.Duration, cmd string, args ...interface{}) (interface{}, error) {
	type timeoutDoer interface {
		DoWithTimeout(time.Duration, string, ...interface{}) (interface{}, error)
	}
	inner, ok := c.Conn.(timeoutDoer)
	if !ok {
		return c.Do(cmd, args...)
	}
	_, sp := c.obs.Start(context.Background(), cmd, summarizeCommand(cmd, args))
	reply, err := c.run(context.Background(), func(context.Context) (interface{}, error) {
		return inner.DoWithTimeout(timeout, cmd, args...)
	})
	sp.End(err)
	return reply, err
}

// run executes call under the resilience executor when armed, mapping redis.ErrNil
// (a cache miss) to success so it never trips the breaker, and surfacing
// protection rejections (rate-limited / circuit-open / bulkhead-full) to the
// caller. When resilience is disabled (exec == nil) call runs directly with no
// overhead. The context passed to call is the executor's per-attempt context
// (which may carry an attempt-timeout); Do/DoWithTimeout ignore it since the
// inner redigo calls take no context, while DoContext threads it through.
func (c *obsConn) run(ctx context.Context, call func(context.Context) (interface{}, error)) (interface{}, error) {
	if c.exec == nil {
		return call(ctx)
	}
	var reply interface{}
	var callErr error
	execErr := c.exec.Execute(ctx, c.resource, func(attemptCtx context.Context) error {
		reply, callErr = call(attemptCtx)
		if callErr != nil && !errors.Is(callErr, redis.ErrNil) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or cache miss
	})
	if execErr != nil {
		if errors.Is(execErr, resilience.ErrRateLimited) ||
			errors.Is(execErr, resilience.ErrCircuitOpen) ||
			errors.Is(execErr, resilience.ErrBulkheadFull) {
			// Rejected before (or around) the command: surface the rejection.
			return nil, execErr
		}
		// A real command error propagated through the executor; callErr holds it.
		return reply, callErr
	}
	return reply, callErr
}

// summarizeCommand renders a short, loggable summary of the command — the
// command name plus the first argument (typically the key) — bounded by the
// Observer's ObserveConfig.MaxArgBytes. The full argument list is intentionally not
// logged (keys are enough to locate an op; values may be sensitive or large).
func summarizeCommand(cmd string, args []interface{}) string {
	if len(args) == 0 {
		return cmd
	}
	return fmt.Sprintf("%s %v", cmd, args[0])
}
