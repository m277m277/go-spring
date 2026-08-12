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
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go-spring.org/cloud/resilience"
	"go-spring.org/stdlib/testing/assert"
)

// newExec builds a default-driver executor with the given policy.
func newExec(t *testing.T, p resilience.Policy) resilience.Executor {
	t.Helper()
	d, err := resilience.GetDriver("default")
	assert.Error(t, err).Nil()
	exec, err := d.NewExecutor(p)
	assert.Error(t, err).Nil()
	t.Cleanup(func() { _ = exec.Close() })
	return exec
}

// countFn returns an operation fn that increments calls and returns a fixed
// error (or nil) so tests can assert how many times the real fn ran.
func countFn(counter *int32, err error) func(context.Context) error {
	return func(context.Context) error {
		atomic.AddInt32(counter, 1)
		return err
	}
}

// TestWrapExecutor_NilInputs confirms the zero-config transparency invariant.
func TestWrapExecutor_NilInputs(t *testing.T) {
	assert.That(t, WrapExecutor(nil, NewInjector(Config{Enabled: true})) == nil).True()
	exec := newExec(t, resilience.Policy{})
	assert.That(t, WrapExecutor(exec, nil) == exec).True()
}

// TestInjector_DisabledIsTransparent: Enabled=false => fn runs, no injection.
func TestInjector_DisabledIsTransparent(t *testing.T) {
	in := NewInjector(Config{}) // Enabled false
	exec := WrapExecutor(newExec(t, resilience.Policy{}), in)
	var calls int32
	err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	assert.Error(t, err).Nil()
	assert.That(t, atomic.LoadInt32(&calls)).Equal(int32(1))
}

// TestInjector_RateZeroAlwaysSucceeds: Rate 0 with Enabled => never inject.
func TestInjector_RateZeroAlwaysSucceeds(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 0, Error: "generic"})
	exec := WrapExecutor(newExec(t, resilience.Policy{}), in)
	var calls int32
	for range 10 {
		err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
		assert.Error(t, err).Nil()
	}
	assert.That(t, atomic.LoadInt32(&calls)).Equal(int32(10))
}

// TestInjector_RateOneRetriesInjectedError: every attempt is injected, so the
// real fn never runs and the executor exhausts its retries (MaxRetries+1
// attempts) and returns the injected error.
func TestInjector_RateOneRetriesInjectedError(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 1, Error: "generic"})
	// MaxRetries 2 => 3 attempts; no breaker so retry is the only path.
	exec := WrapExecutor(newExec(t, resilience.Policy{MaxRetries: 2}), in)
	var calls int32
	err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	assert.Error(t, err).NotNil()
	assert.That(t, IsInjected(err)).True()
	assert.That(t, atomic.LoadInt32(&calls)).Equal(int32(0)) // fn never reached
}

// TestInjectedError_Retryable confirms the Retryable hook is set so resilience
// treats injected faults as retryable.
func TestInjectedError_Retryable(t *testing.T) {
	var r resilience.Retryable
	assert.That(t, errors.As(ErrInjected, &r)).True()
	assert.That(t, r.Retryable()).True()
	assert.That(t, resilience.Policy{}.ShouldRetry(ErrInjected)).True()
}

// TestInjector_KindsSurfaceAsFamiliarErrors: the typed kinds wrap a real error
// so errors.Is matches the underlying sentinel (and the observe outcome mapping
// classifies the call the same way a real timeout/reset would).
func TestInjector_KindsSurfaceAsFamiliarErrors(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 1, Error: "timeout"})
	exec := WrapExecutor(newExec(t, resilience.Policy{}), in)
	err := exec.Execute(context.Background(), "svc", countFn(new(int32), nil))
	assert.Error(t, err).NotNil()
	assert.That(t, errors.Is(err, context.DeadlineExceeded)).True()
	assert.That(t, IsInjected(err)).True()

	in.SetConfig(Config{Enabled: true, Rate: 1, Error: "reset"})
	err = exec.Execute(context.Background(), "svc", countFn(new(int32), nil))
	assert.Error(t, err).NotNil()
	assert.That(t, errors.Is(err, syscall.ECONNRESET)).True()
}

// TestInjector_BreakerOpensUnderFault: with a tight breaker, injected faults
// trip it open and subsequent calls are rejected as ErrCircuitOpen without
// touching fn — the closed loop (fault → breaker → neutral sentinel) holds.
func TestInjector_BreakerOpensUnderFault(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 1, Error: "generic"})
	// ErrorThreshold 1 opens after the first failed attempt; OpenDuration long
	// enough that the second call sees it still open.
	exec := WrapExecutor(newExec(t, resilience.Policy{ErrorThreshold: 1, OpenDuration: time.Second}), in)
	var calls int32
	_ = exec.Execute(context.Background(), "svc", countFn(&calls, nil)) // trips the breaker
	firstCalls := atomic.LoadInt32(&calls)

	err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	assert.Error(t, err).NotNil()
	assert.That(t, errors.Is(err, resilience.ErrCircuitOpen)).True()
	// fn not called on the rejected attempt (only the first call's attempts ran).
	assert.That(t, atomic.LoadInt32(&calls)).Equal(firstCalls)
}

// TestInjector_LatencySleepsAndCancels: injected latency sleeps, and a cancelled
// attempt context surfaces the context error instead of retrying forever.
func TestInjector_LatencySleepsAndCancels(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 0, Latency: 200 * time.Millisecond})
	exec := WrapExecutor(newExec(t, resilience.Policy{Timeout: 50 * time.Millisecond}), in)
	var calls int32
	start := time.Now()
	err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	elapsed := time.Since(start)
	// The 200ms latency exceeds the 50ms per-attempt timeout: the sleep is
	// cancelled by the attempt deadline and the call fails without fn running.
	assert.Error(t, err).NotNil()
	assert.That(t, atomic.LoadInt32(&calls)).Equal(int32(0))
	assert.That(t, elapsed < 200*time.Millisecond).True()
}

// TestInjector_HotSwap: toggling Enabled at runtime via SetConfig takes effect
// on the next operation (the Dync-driven path starters use).
func TestInjector_HotSwap(t *testing.T) {
	in := NewInjector(Config{Enabled: true, Rate: 1, Error: "generic"})
	exec := WrapExecutor(newExec(t, resilience.Policy{}), in)
	var calls int32
	err := exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	assert.That(t, IsInjected(err)).True()

	in.SetConfig(Config{Enabled: false}) // turn the fire off
	err = exec.Execute(context.Background(), "svc", countFn(&calls, nil))
	assert.Error(t, err).Nil()
	assert.That(t, atomic.LoadInt32(&calls)).Equal(int32(1))
}
