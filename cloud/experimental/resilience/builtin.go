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
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// The bundled "default" driver: a self-contained implementation with no
// third-party dependencies, so the framework is usable out of the box and in
// tests. Production deployments select the recommended sentinel-golang driver
// (separate module, registers itself as "sentinel" on blank import) purely by
// changing the driver name — the [Executor] seam and every adapter stay put.
func init() { RegisterDriver("default", builtinDriver{}) }

type builtinDriver struct{}

func (builtinDriver) NewExecutor(p Policy) (Executor, error) {
	if p.RateLimit < 0 {
		return nil, fmt.Errorf("resilience: negative rate limit %v", p.RateLimit)
	}
	return &builtinExecutor{policy: p, states: map[string]*resourceState{}}, nil
}

// builtinExecutor keeps per-resource limiter and breaker state so that one
// misbehaving downstream does not trip protection for the others.
type builtinExecutor struct {
	policy   Policy
	mu       sync.Mutex
	states   map[string]*resourceState
	listener atomic.Pointer[BreakerEventListener]
}

// SetBreakerEventListener attaches l so every per-resource breaker built by this
// executor (including ones created later for new resources) emits state
// transitions. It satisfies [BreakerEventListenerSetter].
func (e *builtinExecutor) SetBreakerEventListener(l BreakerEventListener) {
	e.listener.Store(&l)
}

// Refresh adopts p as the new policy and resets all per-resource state, so the
// next Execute rebuilds breakers/limiters/bulkheads against the new thresholds.
// It satisfies [RefreshableExecutor]. Per-resource state is discarded: a fresh
// breaker/limiter starts from zero, which is the intended semantic of a
// threshold change (the old failure counts were counted under the old policy).
func (e *builtinExecutor) Refresh(p Policy) error {
	if p.RateLimit < 0 {
		return fmt.Errorf("resilience: negative rate limit %v", p.RateLimit)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policy = p
	e.states = map[string]*resourceState{}
	return nil
}

type resourceState struct {
	bucket  *tokenBucket
	breaker *circuitBreaker
	sem     chan struct{}
}

func (e *builtinExecutor) state(resource string) *resourceState {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.states[resource]
	if ok {
		return s
	}
	s = &resourceState{}
	if e.policy.RateLimit > 0 {
		burst := e.policy.Burst
		if burst <= 0 {
			// A small burst keeps steady traffic from being clipped by timing
			// jitter while still bounding spikes.
			if burst = int(e.policy.RateLimit); burst < 1 {
				burst = 1
			}
		}
		s.bucket = newTokenBucket(e.policy.RateLimit, burst)
	}
	if e.policy.BreakerActive() {
		open := e.policy.OpenDuration
		if open <= 0 {
			open = 5 * time.Second
		}
		s.breaker = newCircuitBreaker(e.policy, open, resource, &e.listener)
	}
	if e.policy.MaxConcurrent > 0 {
		// A buffered channel is a non-blocking counting semaphore: a full buffer
		// means the bulkhead is at capacity, so excess calls are rejected rather
		// than queued.
		s.sem = make(chan struct{}, e.policy.MaxConcurrent)
	}
	e.states[resource] = s
	return s
}

func (e *builtinExecutor) Execute(ctx context.Context, resource string, fn func(context.Context) error) error {
	s := e.state(resource)

	// The bulkhead bounds concurrent in-flight calls to the resource; one slot
	// is held for the whole Execute (retries included) and released when done.
	if s.sem != nil {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		default:
			return ErrBulkheadFull
		}
	}

	// MaxDuration caps the whole call across retries. It only tightens the
	// caller's own context; per-attempt Timeout is applied inside runOnce
	// against budgetCtx, so the effective per-attempt bound is min(Timeout,
	// remaining MaxDuration).
	budgetCtx := ctx
	if e.policy.MaxDuration > 0 {
		var cancel context.CancelFunc
		budgetCtx, cancel = context.WithTimeout(ctx, e.policy.MaxDuration)
		defer cancel()
	}

	attempts := e.policy.MaxRetries + 1
	var err error
	for i := range attempts {
		if s.bucket != nil && !s.bucket.allowN(1) {
			return ErrRateLimited
		}
		if s.breaker != nil && !s.breaker.allow() {
			return ErrCircuitOpen
		}

		err = e.runOnce(budgetCtx, fn)

		if s.breaker != nil {
			s.breaker.record(err == nil)
		}
		if err == nil {
			return nil
		}
		// A cancelled/Expired budget (caller cancel or MaxDuration) ends the
		// loop before the predicate is consulted: there is no budget left for
		// another attempt regardless of why the last one failed.
		if budgetCtx.Err() != nil {
			break
		}
		if !e.policy.ShouldRetry(err) {
			break
		}
		if i == attempts-1 {
			break // last attempt — no backoff sleep after it
		}
		if !SleepFor(budgetCtx, e.policy.Backoff(i)) {
			break // backoff interrupted by ctx cancellation
		}
	}
	return err
}

// runOnce applies the per-attempt timeout, if any, around fn. The ctx it
// receives is already bounded by the Execute-level MaxDuration budget.
func (e *builtinExecutor) runOnce(ctx context.Context, fn func(context.Context) error) error {
	if e.policy.Timeout <= 0 {
		return fn(ctx)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, e.policy.Timeout)
	defer cancel()
	return fn(attemptCtx)
}

func (e *builtinExecutor) Close() error { return nil }

// tokenBucket is a minimal, dependency-free rate limiter. Tokens refill
// continuously at rate per second up to burst.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   time.Now(),
	}
}

// circuitBreaker supports two strategies over one open/half-open state machine:
//
//   - consecutive: trips after threshold failures in a row (a success resets
//     the count), the historical behavior.
//   - error-rate: trips when fails/total over a rolling window reaches a ratio,
//     once a minimum sample has accumulated.
//
// Half-open admits exactly one trial: the gate is a single-permit channel, not a
// bool flag, so two concurrent callers arriving right after cool-down cannot
// both be admitted (the prior flag-based implementation had exactly that race).
type circuitBreaker struct {
	strategy BreakerStrategy
	openFor  time.Duration

	// consecutive strategy
	threshold int

	// error-rate strategy
	rateThreshold float64 // fails/total ratio that trips
	minRequests   int     // minimum sample before tripping
	window        time.Duration

	mu       sync.Mutex
	failures int // consecutive counter
	openedAt time.Time
	halfOpen chan struct{} // non-nil while a single trial permit is offered

	// rolling-window counters for the error-rate strategy
	win       time.Duration
	curStart  time.Time
	total     int
	fails     int
	prevTotal int
	prevFails int

	// event emission
	resource string                                  // label passed to the listener
	listener *atomic.Pointer[BreakerEventListener]   // shared with the executor; nil ptr or nil value = no listener
}

func newCircuitBreaker(p Policy, open time.Duration, resource string, listener *atomic.Pointer[BreakerEventListener]) *circuitBreaker {
	c := &circuitBreaker{
		strategy:      p.ResolvedBreakerStrategy(),
		openFor:       open,
		threshold:     p.ErrorThreshold,
		rateThreshold: p.ErrorRateThreshold,
		minRequests:   p.MinRequests,
		resource:      resource,
		listener:      listener,
	}
	if c.strategy == BreakerErrorRate {
		if c.minRequests <= 0 {
			c.minRequests = 1
		}
		if p.BreakerWindow > 0 {
			c.win = p.BreakerWindow
		} else {
			c.win = time.Second
		}
		c.curStart = time.Now()
	}
	return c
}

// allow reports whether a request may proceed given the current breaker state.
func (c *circuitBreaker) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openedAt.IsZero() {
		return true // closed
	}
	if time.Since(c.openedAt) < c.openFor {
		return false // open, cooling down
	}
	// Cool-down elapsed: open a single-permit half-open gate if none yet, then
	// take the only permit. A concurrent caller finds the gate empty and is
	// treated as still-open — exactly one trial is admitted.
	if c.halfOpen == nil {
		c.halfOpen = make(chan struct{}, 1)
		c.halfOpen <- struct{}{}
		c.notifyLocked(BreakerOpen, BreakerHalfOpen)
	}
	select {
	case <-c.halfOpen:
		return true
	default:
		return false
	}
}

// record folds an attempt's outcome back into the breaker state.
func (c *circuitBreaker) record(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Half-open trial resolves first: its outcome closes or re-opens the
	// circuit regardless of strategy.
	if c.halfOpen != nil {
		if success {
			c.closeLocked()
			c.notifyLocked(BreakerHalfOpen, BreakerClosed)
			return
		}
		// Trial failed: re-open the cool-down window.
		c.halfOpen = nil
		c.openedAt = time.Now()
		c.notifyLocked(BreakerHalfOpen, BreakerOpen)
		return
	}
	if c.strategy == BreakerErrorRate {
		c.recordRate(success)
		return
	}
	c.recordConsecutive(success)
}

func (c *circuitBreaker) recordConsecutive(success bool) {
	if success {
		c.failures = 0
		c.openedAt = time.Time{}
		return
	}
	c.failures++
	if c.failures >= c.threshold {
		if c.openedAt.IsZero() {
			c.notifyLocked(BreakerClosed, BreakerOpen)
		}
		c.openedAt = time.Now()
	}
}

// recordRate advances the rolling-window counters and trips the breaker when
// the failure ratio reaches rateThreshold with enough samples. It uses the same
// weighted two-window estimate as the standalone slidingWindow limiter.
func (c *circuitBreaker) recordRate(success bool) {
	now := time.Now()
	elapsed := now.Sub(c.curStart)
	if elapsed >= c.win {
		if elapsed >= 2*c.win {
			c.prevTotal, c.prevFails = 0, 0
		} else {
			c.prevTotal, c.prevFails = c.total, c.fails
		}
		c.total, c.fails = 0, 0
		c.curStart = now
		elapsed = 0
	}
	c.total++
	if !success {
		c.fails++
	}
	weight := float64(c.win-elapsed) / float64(c.win)
	estimateTotal := float64(c.prevTotal)*weight + float64(c.total)
	estimateFails := float64(c.prevFails)*weight + float64(c.fails)
	if c.total+c.prevTotal >= c.minRequests && estimateTotal > 0 &&
		estimateFails/estimateTotal >= c.rateThreshold {
		if c.openedAt.IsZero() {
			c.notifyLocked(BreakerClosed, BreakerOpen)
		}
		c.openedAt = now
	}
}

func (c *circuitBreaker) closeLocked() {
	c.failures = 0
	c.openedAt = time.Time{}
	c.halfOpen = nil
	// Reset the windowed counters so a freshly-closed breaker starts clean.
	c.total, c.fails = 0, 0
	c.prevTotal, c.prevFails = 0, 0
	c.curStart = time.Now()
}

// notifyLocked emits a from→to transition to the listener, if one is attached.
// Caller holds mu; the listener must not call back into the breaker/executor
// (documented on [BreakerEventListener]).
func (c *circuitBreaker) notifyLocked(from, to BreakerState) {
	if from == to || c.listener == nil {
		return
	}
	if l := c.listener.Load(); l != nil {
		(*l).OnBreakerStateChange(c.resource, from, to)
	}
}

