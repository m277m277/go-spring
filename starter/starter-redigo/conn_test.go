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
	"testing"

	"github.com/gomodule/redigo/redis"
)

// stubConn is a minimal redis.Conn recording the commands its Do receives.
type stubConn struct {
	redis.Conn
	calls []string // one entry per Do cmd
	reply interface{}
	err   error
}

func (s *stubConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	s.calls = append(s.calls, cmd)
	return s.reply, s.err
}
func (s *stubConn) Close() error                      { return nil }
func (s *stubConn) Err() error                        { return nil }
func (s *stubConn) Send(string, ...interface{}) error { return nil }
func (s *stubConn) Flush() error                      { return nil }
func (s *stubConn) Receive() (interface{}, error)     { return nil, nil }

// newConn builds a Conn wired to inner with the given interceptor and no
// observe / no resilience — the minimal harness for command-path tests.
func newConn(inner redis.Conn, hook CommandInterceptor) *Conn {
	return &Conn{Conn: inner, interceptor: hook}
}

// A short-circuiting interceptor must return its canned reply WITHOUT calling
// next, so the underlying conn is never reached.
func TestConn_InterceptorShortCircuitSkipsRedis(t *testing.T) {
	inner := &stubConn{reply: "from-redis"}
	calledNext := false
	hook := CommandInterceptor(func(ctx context.Context, cmd string, args []interface{},
		next func(context.Context) (interface{}, error)) (interface{}, error) {
		if cmd == "GET" {
			return "cached", nil // local hit — do not call next
		}
		calledNext = true
		return next(ctx)
	})
	c := newConn(inner, hook)

	reply, err := c.Do("GET", "k")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if reply != "cached" {
		t.Fatalf("reply = %v, want cached short-circuit", reply)
	}
	if len(inner.calls) != 0 {
		t.Fatalf("inner conn reached (%v); short-circuit must skip Redis", inner.calls)
	}
	if calledNext {
		t.Fatal("interceptor called next for GET; short-circuit must not")
	}
}

// An observer-style interceptor calls next; the command reaches the inner conn
// and its result flows back. A non-short-circuited cmd also reaches the conn.
func TestConn_InterceptorCallingNextReachesRedis(t *testing.T) {
	inner := &stubConn{reply: "from-redis"}
	hook := CommandInterceptor(func(ctx context.Context, cmd string, args []interface{},
		next func(context.Context) (interface{}, error)) (interface{}, error) {
		reply, err := next(ctx)
		if err != nil {
			return nil, err
		}
		return "wrapped:" + reply.(string), nil
	})
	c := newConn(inner, hook)

	reply, err := c.Do("SET", "k", "v")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if reply != "wrapped:from-redis" {
		t.Fatalf("reply = %v, want wrapped:from-redis", reply)
	}
	if len(inner.calls) != 1 || inner.calls[0] != "SET" {
		t.Fatalf("inner calls = %v, want exactly [SET]", inner.calls)
	}
}

// With no interceptor the built-in path runs directly and reaches the conn.
func TestConn_NoInterceptorReachesRedis(t *testing.T) {
	inner := &stubConn{reply: 42}
	c := newConn(inner, nil)

	reply, err := c.Do("INCR", "counter")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if reply != 42 {
		t.Fatalf("reply = %v, want 42", reply)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %v, want one", inner.calls)
	}
}

// With observe disabled (obs == nil) and no resilience, the Conn wrapper is a
// pass-through: the command still reaches the inner conn and no span-related
// nil-deref occurs. Proves the "observe off keeps wrapping transparent" rule.
func TestConn_ObserveDisabledPassThrough(t *testing.T) {
	inner := &stubConn{reply: "ok"}
	c := &Conn{Conn: inner, obs: nil, exec: nil, interceptor: nil}

	reply, err := c.Do("PING")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %v, want ok", reply)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("inner calls = %v, want one", inner.calls)
	}
}

// RegisterInterceptor sets the single global slot; a second call panics. This
// test registers once and asserts the duplicate-panic contract via recover, so
// it leaves the package global set — it must run alone in its own goroutine-free
// ordering, hence the defer cleanup restoring the global.
func TestRegisterInterceptor_PanicsOnDuplicate(t *testing.T) {
	saved := interceptor
	interceptor = nil
	defer func() { interceptor = saved }()

	RegisterInterceptor(func(context.Context, string, []interface{},
		func(context.Context) (interface{}, error)) (interface{}, error) {
		return nil, errors.New("unused")
	})

	var got any
	func() {
		defer func() { got = recover() }()
		RegisterInterceptor(func(context.Context, string, []interface{},
			func(context.Context) (interface{}, error)) (interface{}, error) {
			return nil, nil
		})
	}()
	if got == nil {
		t.Fatal("second RegisterInterceptor should panic")
	}
}
