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

// Package resilience defines a framework-agnostic, zero-dependency abstraction
// for client-side fault tolerance: rate limiting, circuit breaking, retry and
// per-attempt timeout.
//
// It answers one question for outbound calls (HTTP, RPC, DB, cache, ...):
// "before I let this operation run, should it be throttled, short-circuited or
// retried?". It says nothing about which library makes the call — each client
// starter plugs the single [Executor] seam into its own request hook
// (http.RoundTripper, redis.Hook, gorm plugin, ...).
//
// The abstraction is split from its implementations exactly like
// [go-spring.org/spring/discovery]:
//
//   - [Policy] is a backend-neutral, declarative description of the desired
//     protection.
//   - [Driver] turns a Policy into a live [Executor]. A company (or the bundled
//     builtin) implements Driver once and registers it via [RegisterDriver];
//     callers select it by name through [MustGetDriver] with no per-component
//     adaptation.
//   - The bundled "default" driver (see builtin.go) has zero third-party
//     dependencies so the framework runs standalone; the recommended
//     production driver (sentinel-golang) lives in its own module and registers
//     itself on blank import.
package resilience

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"time"
)

// ErrRateLimited is returned (or wrapped) by an [Executor] when an operation is
// rejected because the configured rate limit is exceeded.
var ErrRateLimited = errors.New("resilience: rate limited")

// ErrCircuitOpen is returned (or wrapped) by an [Executor] when an operation is
// rejected because the circuit breaker for its resource is open.
var ErrCircuitOpen = errors.New("resilience: circuit open")

// ErrBulkheadFull is returned (or wrapped) by an [Executor] when an operation is
// rejected because the resource already has the maximum number of concurrent
// in-flight operations allowed by the bulkhead.
var ErrBulkheadFull = errors.New("resilience: bulkhead full")

// BreakerStrategy selects how the circuit breaker counts failures. Each [Driver]
// maps it onto its own primitives; the builtin driver implements both directly,
// sentinel-golang translates them onto its ErrorCount / ErrorRatio rules.
type BreakerStrategy string

const (
	// BreakerConsecutive trips after [Policy.ErrorThreshold] failures in a row
	// (any success resets the count). It is the default when BreakerStrategy is
	// empty, matching the historical behavior.
	BreakerConsecutive BreakerStrategy = "consecutive"
	// BreakerErrorRate trips when the failure ratio over [Policy.BreakerWindow]
	// reaches [Policy.ErrorRateThreshold], once at least [Policy.MinRequests]
	// requests have been observed in the window. Use it for high-throughput
	// resources where a burst of failures or a steady partial-failure rate is
	// more meaningful than a consecutive run.
	BreakerErrorRate BreakerStrategy = "error-rate"
)

// Policy is a backend-neutral description of the protection wanted for a set of
// operations. Each [Driver] maps these knobs onto its own primitives (the
// builtin driver reads them directly; sentinel-golang translates them into its
// flow/circuit-breaker rules). A zero Policy protects nothing — every stage is
// opt-in, so an unset Policy makes [Executor.Execute] a transparent pass-through.
//
// Every field below is zero-value-safe: a zero field keeps the historical
// behavior, so existing Policy literals and configs are unaffected by the newer
// knobs (backoff, retry classification, total budget, breaker strategy).
type Policy struct {
	// RateLimit caps sustained throughput in operations per second. 0 disables
	// rate limiting.
	RateLimit float64

	// Burst is the maximum number of operations allowed to exceed RateLimit
	// momentarily. It defaults to a small multiple of RateLimit when unset; it
	// is ignored when RateLimit is 0.
	Burst int

	// ErrorThreshold is the failure count that trips the circuit breaker. Under
	// [BreakerConsecutive] (the default) it counts failures in a row; under
	// [BreakerErrorRate] it is unused (see [Policy.ErrorRateThreshold]). 0
	// disables the consecutive strategy.
	ErrorThreshold int

	// OpenDuration is how long the circuit stays open before a trial request is
	// allowed through (half-open). Ignored when no breaker strategy is active;
	// defaults to a few seconds when unset.
	OpenDuration time.Duration

	// BreakerStrategy selects how the breaker counts failures (consecutive vs.
	// error-rate). Empty means [BreakerConsecutive]. The breaker is active when
	// [Policy.ErrorThreshold] > 0 (consecutive) or [Policy.ErrorRateThreshold]
	// > 0 (error-rate).
	BreakerStrategy BreakerStrategy

	// ErrorRateThreshold is the failure ratio in (0,1] that trips an
	// [BreakerErrorRate] breaker. 0 disables the rate strategy. Pair with
	// [Policy.MinRequests] and [Policy.BreakerWindow].
	ErrorRateThreshold float64

	// MinRequests is the minimum sample size observed in [Policy.BreakerWindow]
	// before an [BreakerErrorRate] breaker may trip, so a 1/1 failure does not
	// open the circuit. It defaults to 1 when unset; ignored by the consecutive
	// strategy.
	MinRequests int

	// BreakerWindow is the rolling interval the error-rate strategy counts
	// over. It defaults to one second when unset. The builtin consecutive
	// breaker ignores it (it counts an unbounded run); sentinel uses it as the
	// stat interval for both strategies.
	BreakerWindow time.Duration

	// MaxConcurrent caps the number of operations allowed to run against a
	// resource at the same time (the bulkhead / isolation stage). Excess calls
	// are rejected with [ErrBulkheadFull] rather than queued, so a slow
	// downstream cannot exhaust the caller's goroutines or connections. 0
	// disables the bulkhead.
	MaxConcurrent int

	// MaxRetries is the number of extra attempts after the first failure. 0
	// means a single attempt with no retry. Retries respect the circuit breaker,
	// rate limiter and bulkhead, and are paced by the backoff fields below.
	MaxRetries int

	// RetryPredicate reports whether err should trigger another attempt. A nil
	// predicate retries on every non-nil error (the historical behavior): set it
	// to [DefaultRetryPredicate] for a safe, network-classification default, or
	// supply your own for protocol-specific classification (e.g. only retry
	// idempotent verbs). It is consulted after each attempt; a [Retryable] error
	// overrides it.
	RetryPredicate func(err error) bool

	// InitialInterval is the backoff before the first retry. 0 disables backoff
	// entirely (retries run back-to-back, the historical behavior). Pair with
	// [Policy.Multiplier], [Policy.MaxInterval] and [Policy.RandomizationFactor].
	InitialInterval time.Duration

	// Multiplier is the exponential growth factor applied to InitialInterval
	// between successive retries. 0 and 1 both mean a constant interval.
	Multiplier float64

	// MaxInterval caps the grown backoff so it does not expand unbounded. 0
	// means no cap.
	MaxInterval time.Duration

	// RandomizationFactor is the jitter fraction in [0,1) applied to each
	// computed interval as interval*(1 ± factor), decorrelating clients that
	// would otherwise retry in lockstep. 0 means no jitter.
	RandomizationFactor float64

	// Timeout bounds each individual attempt via a derived context. 0 means no
	// per-attempt timeout is imposed by the executor.
	Timeout time.Duration

	// MaxDuration caps the wall time of the whole [Executor.Execute] across all
	// retries. When both [Policy.Timeout] and MaxDuration are set, the effective
	// per-attempt budget is the smaller of Timeout and the remaining MaxDuration.
	// 0 means no total cap (beyond the caller's own context deadline).
	MaxDuration time.Duration
}

// IsZero reports whether p configures no protection — every stage disabled, so
// [Executor.Execute] would be a transparent pass-through. It replaces struct
// equality (`p == (Policy{})`) which is no longer possible now that Policy
// carries a function field ([Policy.RetryPredicate]). Callers that previously
// wrote `p == (Policy{})` to mean "no policy configured" should use IsZero.
//
// RetryPredicate is intentionally not consulted: with MaxRetries == 0 no retry
// runs, so a bare predicate set on an otherwise-zero Policy protects nothing.
func (p Policy) IsZero() bool {
	return p.RateLimit == 0 && p.Burst == 0 &&
		p.ErrorThreshold == 0 && p.OpenDuration == 0 &&
		p.BreakerStrategy == "" && p.ErrorRateThreshold == 0 &&
		p.MinRequests == 0 && p.BreakerWindow == 0 &&
		p.MaxConcurrent == 0 && p.MaxRetries == 0 &&
		p.InitialInterval == 0 && p.Multiplier == 0 &&
		p.MaxInterval == 0 && p.RandomizationFactor == 0 &&
		p.Timeout == 0 && p.MaxDuration == 0
}

// ResolvedBreakerStrategy returns the strategy a driver should apply, defaulting
// to [BreakerConsecutive] when [Policy.BreakerStrategy] is empty. It is shared by
// both drivers so they always agree on the resolution.
func (p Policy) ResolvedBreakerStrategy() BreakerStrategy {
	if p.BreakerStrategy == BreakerErrorRate {
		return BreakerErrorRate
	}
	return BreakerConsecutive
}

// BreakerActive reports whether any breaker strategy is configured (a consecutive
// threshold or an error-rate threshold is set). Both drivers consult it so they
// build a breaker under exactly the same condition.
func (p Policy) BreakerActive() bool {
	return p.ErrorThreshold > 0 || p.ErrorRateThreshold > 0
}

// Retryable is implemented by an error to opt itself in or out of retry,
// overriding [Policy.RetryPredicate] when the error type is known to the caller
// but not to the executor. This is how a gorm/redis adapter marks a
// non-idempotent-write error (Retryable() false) or a transient one (true)
// without teaching the executor about that client library.
type Retryable interface {
	Retryable() bool
}

// DefaultRetryPredicate retries on transient/network failures and deliberately
// suppresses retry on caller cancellation ([context.Canceled]) and on
// non-network errors (which usually signal a definite "no" — bad request,
// validation, auth). It is a safe default for idempotent reads; non-idempotent
// writes should pass a stricter predicate or return a [Retryable] error.
//
// It is opt-in: a zero [Policy.RetryPredicate] keeps the historical "retry on
// every error" behavior. Assign it explicitly when you want this classification.
func DefaultRetryPredicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	// A per-attempt timeout surfaces as DeadlineExceeded and is retryable; a
	// caller-wide deadline expiry also surfaces as DeadlineExceeded but the loop
	// has already stopped on ctx.Err() before consulting the predicate.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	var s *httpStatusError
	if errors.As(err, &s) {
		return s.retryable
	}
	return false
}

// Executor runs operations under a [Policy]. It is the single seam every client
// adapter calls; implementations must be safe for concurrent use.
type Executor interface {
	// Execute runs fn under the policy, scoping rate-limiter and circuit-breaker
	// state to resource (typically a downstream service name). It returns
	// [ErrRateLimited] or [ErrCircuitOpen] when the call is rejected before fn
	// runs, or fn's own (final) error otherwise. The context passed to fn may be
	// a per-attempt timeout derived from ctx.
	Execute(ctx context.Context, resource string, fn func(context.Context) error) error

	// Close releases any background resources held by the executor (e.g. metric
	// pumps in a production driver). It is safe to call more than once.
	Close() error
}

// Driver builds an [Executor] from a [Policy]. Backends implement it and
// register under a name via [RegisterDriver].
type Driver interface {
	NewExecutor(Policy) (Executor, error)
}

// Fallback runs fn through exec and, when the operation is rejected (rate
// limited, circuit open, bulkhead full) or fails after all retries, invokes
// degrade to produce a graceful result instead of surfacing the error. It is
// the degradation stage of the framework and composes with any [Executor]
// regardless of driver: degrade receives the triggering error so it can serve
// cached data for [ErrCircuitOpen] yet propagate a genuine bug, for example.
//
// degrade's own error (or nil) becomes the final result. When exec is nil the
// call is a transparent pass-through: fn runs once and its error, if any, still
// reaches degrade, so wiring stays a no-op until a policy is configured.
func Fallback(ctx context.Context, exec Executor, resource string,
	fn func(context.Context) error, degrade func(context.Context, error) error) error {
	var err error
	if exec == nil {
		err = fn(ctx)
	} else {
		err = exec.Execute(ctx, resource, fn)
	}
	if err == nil {
		return nil
	}
	return degrade(ctx, err)
}
