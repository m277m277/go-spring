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

// Package loadtest is a small load-test harness shared by the go-spring client
// starters' example-load binaries. It fans an [Op] out across N workers for a
// fixed duration, collects per-op latency into per-worker slices (no shared lock
// on the hot path), then merges them for percentiles and classifies errors into
// the resilience + fault taxonomy (circuit-open / rate-limited / bulkhead /
// injected / other).
//
// It is example-grade tooling, not production code: each example-load main wires
// one starter-specific [Op] (a SET/GET, a SQL query, a Mongo find, ...) and hands
// it to [Run]; the harness does the fan-out, timing and reporting.
package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
)

// Op is one operation a worker runs per iteration. It returns nil on success.
// The context is the run's context (cancelled when the duration elapses).
type Op func(ctx context.Context) error

// Config controls a load run.
type Config struct {
	Concurrency int           // number of worker goroutines
	Duration    time.Duration // how long to drive load before stopping
}

// Result is the aggregate of a [Run]. Latencies is sorted ascending so
// [Result.Percentile] can index directly.
type Result struct {
	Ops       int64
	Elapsed   time.Duration
	QPS       float64
	Latencies []time.Duration

	// Error buckets, classified via the resilience sentinels + fault.IsInjected.
	Circuit     int64
	RateLimited int64
	Bulkhead    int64
	Injected    int64
	Other       int64
}

// Errors reports the total across all error buckets.
func (r *Result) Errors() int64 {
	return r.Circuit + r.RateLimited + r.Bulkhead + r.Injected + r.Other
}

// Percentile returns the p-quantile (0..1) of the latency sample, or 0 when the
// run produced no samples.
func (r *Result) Percentile(p float64) time.Duration {
	if len(r.Latencies) == 0 {
		return 0
	}
	return r.Latencies[int(float64(len(r.Latencies)-1)*p)]
}

// Run drives op across cfg.Concurrency workers for cfg.Duration, then merges and
// classifies the samples into a [Result]. It returns when the duration elapses
// (or ctx is cancelled). op is invoked sequentially per worker; workers do not
// share latency state, so the hot path is lock-free.
func Run(ctx context.Context, cfg Config, op Op) *Result {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	n := cfg.Concurrency
	locals := make([][]time.Duration, n)

	var (
		ops                              int64
		circuit, rate, bulk, injected, other int64
	)
	classify := func(err error) {
		switch {
		case errors.Is(err, resilience.ErrCircuitOpen):
			atomic.AddInt64(&circuit, 1)
		case errors.Is(err, resilience.ErrRateLimited):
			atomic.AddInt64(&rate, 1)
		case errors.Is(err, resilience.ErrBulkheadFull):
			atomic.AddInt64(&bulk, 1)
		case fault.IsInjected(err):
			atomic.AddInt64(&injected, 1)
		default:
			atomic.AddInt64(&other, 1)
		}
	}

	deadline := time.Now().Add(cfg.Duration)
	start := time.Now()
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			local := make([]time.Duration, 0, 1024)
			for {
				if ctx.Err() != nil {
					break
				}
				if !time.Now().Before(deadline) {
					break
				}
				opStart := time.Now()
				err := op(ctx)
				local = append(local, time.Since(opStart))
				atomic.AddInt64(&ops, 1)
				if err != nil {
					classify(err)
				}
			}
			locals[id] = local
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	all := make([]time.Duration, 0, ops)
	for _, l := range locals {
		all = append(all, l...)
	}
	slices.Sort(all)

	qps := 0.0
	if elapsed > 0 {
		qps = float64(ops) / elapsed.Seconds()
	}
	return &Result{
		Ops:         ops,
		Elapsed:     elapsed,
		QPS:         qps,
		Latencies:   all,
		Circuit:     atomic.LoadInt64(&circuit),
		RateLimited: atomic.LoadInt64(&rate),
		Bulkhead:    atomic.LoadInt64(&bulk),
		Injected:    atomic.LoadInt64(&injected),
		Other:       atomic.LoadInt64(&other),
	}
}

// Print writes a human-readable report (config, throughput, latency
// percentiles, error breakdown) to w.
func (r *Result) Print(w io.Writer) {
	errs := r.Errors()
	errPct := 0.0
	if r.Ops > 0 {
		errPct = float64(errs) / float64(r.Ops) * 100
	}
	fmt.Fprintf(w, "================ load report ================\n")
	fmt.Fprintf(w, "ops=%d  qps=%.0f  elapsed=%s  errors=%d (%.2f%%)\n",
		r.Ops, r.QPS, r.Elapsed.Truncate(time.Millisecond), errs, errPct)
	if len(r.Latencies) > 0 {
		fmt.Fprintf(w, "latency  p50=%s  p90=%s  p99=%s  p999=%s  max=%s\n",
			r.Percentile(0.50), r.Percentile(0.90), r.Percentile(0.99),
			r.Percentile(0.999), r.Latencies[len(r.Latencies)-1])
	}
	fmt.Fprintf(w, "errors by class:  circuit=%d  rate-limited=%d  bulkhead=%d  injected=%d  other=%d\n",
		r.Circuit, r.RateLimited, r.Bulkhead, r.Injected, r.Other)
	fmt.Fprintf(w, "=============================================\n")
}
