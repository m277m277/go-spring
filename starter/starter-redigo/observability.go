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
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	observe "go-spring.org/observe"
)

// applyObservability wraps the pool's Dial / DialContext so every connection
// handed out is an obsConn that instruments each command through the shared
// observe kit (trace span + duration/in-flight metric + access log). It replaces
// the manual StartRedisSpan call-site helper: instrumentation is now transparent
// — application call sites are unchanged.
//
// The kit rides the OTel globals (starter-otel) and the project log, so this is
// a near-zero-cost opt-in that needs no per-component adaptation: when
// starter-otel is absent, trace+metric are no-ops; the access log is gated by
// cfg.Level (default brief).
func applyObservability(pool *redis.Pool, cfg observe.LogConfig) {
	if pool == nil {
		return
	}
	obs := observe.NewClient("redis", cfg)
	wrap := func(c redis.Conn) redis.Conn {
		if c == nil {
			return nil
		}
		return &obsConn{Conn: c, obs: obs}
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

// obsConn wraps a redis.Conn so every command flows through the observe kit.
//
// Do is the common path and is fully instrumented. DoContext / DoWithTimeout
// (the optional ConnWithContext / ConnWithTimeout interfaces) are implemented so
// the wrapper is transparent to apps and helpers that type-assert those
// interfaces (redis.DoContext etc.) — when the inner conn supports them, the
// call is instrumented with the caller's context (so the client span links to
// the request trace); when it does not, the call falls back to the plain Do path.
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
	obs *observe.Observer
}

func (c *obsConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	_, sp := c.obs.Start(context.Background(), cmd, doArg(cmd, args))
	reply, err := c.Conn.Do(cmd, args...)
	sp.End(err)
	return reply, err
}

// DoContext instruments the context-aware path and links the span to the caller.
func (c *obsConn) DoContext(ctx context.Context, cmd string, args ...interface{}) (interface{}, error) {
	type ctxDoer interface {
		DoContext(context.Context, string, ...interface{}) (interface{}, error)
	}
	if inner, ok := c.Conn.(ctxDoer); ok {
		ctx, sp := c.obs.Start(ctx, cmd, doArg(cmd, args))
		reply, err := inner.DoContext(ctx, cmd, args...)
		sp.End(err)
		return reply, err
	}
	// Inner conn has no DoContext — best-effort fallback to the plain path.
	return c.Do(cmd, args...)
}

// DoWithTimeout instruments the timeout variant. redigo's ConnWithTimeout has no
// context, so the span is a root span (same caveat as Do).
func (c *obsConn) DoWithTimeout(timeout time.Duration, cmd string, args ...interface{}) (interface{}, error) {
	type timeoutDoer interface {
		DoWithTimeout(time.Duration, string, ...interface{}) (interface{}, error)
	}
	if inner, ok := c.Conn.(timeoutDoer); ok {
		_, sp := c.obs.Start(context.Background(), cmd, doArg(cmd, args))
		reply, err := inner.DoWithTimeout(timeout, cmd, args...)
		sp.End(err)
		return reply, err
	}
	return c.Do(cmd, args...)
}

// doArg renders a short, loggable summary of the command — the command name plus
// the first argument (typically the key) — bounded by the Observer's
// LogConfig.MaxArgBytes. The full argument list is intentionally not logged
// (keys are enough to locate an op; values may be sensitive or large).
func doArg(cmd string, args []interface{}) string {
	if len(args) == 0 {
		return cmd
	}
	return fmt.Sprintf("%s %v", cmd, args[0])
}
