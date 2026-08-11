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

// Package resilobserve is the shared resilience-instrumentation adapter for the
// go-spring observability story. It wraps any [resilience.Executor] so each
// Execute emits a trace span + call counter (classified by outcome) + duration
// histogram + access log — making circuit-breaker trips, rate-limit rejects and
// bulkhead rejections visible in production instead of being a black box, which
// is the gap the core resilience package leaves by design (it deliberately does
// no metric/trace/log).
//
// It lives in its own module for the same reason observe-gorm / observe-lock /
// observe-transaction exist: the otel-free spring core defines the
// [resilience.Executor] interface, and the instrumentation belongs beside the
// adapters, not in core. A client starter that already builds an Executor wraps
// it once at construction:
//
//	exec = resilobserve.WrapExecutor(exec, "redis", c.Observability)
//
// Everything rides the OTel globals starter-otel installs; without it the
// global tracer/meter are no-ops, so the wrapper adds negligible overhead and
// changes no behaviour. Only the access log always emits (gated by cfg.Level).
package resilobserve

import (
	"context"
	"errors"
	"time"

	"go-spring.org/log"
	observe "go-spring.org/observe"
	"go-spring.org/spring/experimental/cloud/resilience"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies the OTel tracer/meter for this kit.
const tracerName = "go-spring.org/observe-resilience"

// durationBuckets mirror observe's client-op buckets so resilience latency
// shares the same scale as the downstream client calls it protects.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// WrapExecutor returns a [resilience.Executor] that wraps inner, emitting a
// trace span + call counter + duration histogram + access log for each Execute,
// with the outcome classified as one of: success, rate_limited, circuit_open,
// bulkhead_full, timeout, error. Pass the system label (e.g. "redis", "gorm",
// "grpc") so metrics from several protected clients are distinguishable. cfg
// controls the access log (off/brief/detailed). A nil inner returns nil — no
// wrapper, so an unarmed client stays untouched.
func WrapExecutor(inner resilience.Executor, system string, cfg observe.LogConfig) resilience.Executor {
	if inner == nil {
		return nil
	}
	meter := otel.Meter(tracerName)
	calls, _ := meter.Int64Counter("resilience.calls",
		metric.WithDescription("Number of resilience-protected calls by outcome"),
		metric.WithUnit("{call}"))
	duration, _ := meter.Float64Histogram("resilience.operation.duration",
		metric.WithDescription("Duration of resilience-protected "+system+" calls"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	active, _ := meter.Int64UpDownCounter("resilience.active_requests",
		metric.WithDescription("Number of in-flight resilience-protected "+system+" calls"),
		metric.WithUnit("{request}"))
	breakerChanges, _ := meter.Int64Counter("resilience.breaker.state_change",
		metric.WithDescription("Circuit-breaker state transitions (from/to attrs)"),
		metric.WithUnit("{event}"))
	w := &wrappedExecutor{
		inner:          inner,
		system:         system,
		tracer:         otel.Tracer(tracerName),
		calls:          calls,
		duration:       duration,
		active:         active,
		breakerChanges: breakerChanges,
		logTag:         log.RegisterInfraTag(system, "resilience"),
		cfg:            cfg,
	}
	// If the inner executor emits breaker state transitions, subscribe so each
	// trip / half-open / recovery emits a counter + log automatically. Drivers
	// without that capability (no BreakerEventListenerSetter) are silently
	// skipped — the call-level signals still emit.
	if setter, ok := inner.(resilience.BreakerEventListenerSetter); ok {
		setter.SetBreakerEventListener(w)
	}
	return w
}

type wrappedExecutor struct {
	inner          resilience.Executor
	system         string
	tracer         trace.Tracer
	calls          metric.Int64Counter
	duration       metric.Float64Histogram
	active         metric.Int64UpDownCounter
	breakerChanges metric.Int64Counter
	logTag         *log.Tag
	cfg            observe.LogConfig
}

// OnBreakerStateChange satisfies [resilience.BreakerEventListener]. It is
// invoked synchronously from inside the breaker's transition (so it must not
// call back into the executor); it emits a state-change counter and a log line.
func (w *wrappedExecutor) OnBreakerStateChange(resource string, from, to resilience.BreakerState) {
	w.breakerChanges.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("resilience.system", w.system),
		attribute.String("resilience.resource", resource),
		attribute.String("from", from.String()),
		attribute.String("to", to.String()),
	))
	if w.cfg.Level != "off" {
		fields := []log.Field{
			log.String("resilience.resource", resource),
			log.String("from", from.String()),
			log.String("to", to.String()),
		}
		// A trip (→open) is a service-level degradation worth flagging at Warn;
		// recovery and half-open trial are Info.
		if to == resilience.BreakerOpen {
			log.Warn(context.Background(), w.logTag, fields...)
		} else {
			log.Info(context.Background(), w.logTag, fields...)
		}
	}
}

func (w *wrappedExecutor) Execute(ctx context.Context, resource string, fn func(context.Context) error) error {
	baseAttrs := []attribute.KeyValue{
		attribute.String("resilience.system", w.system),
		attribute.String("resilience.resource", resource),
	}
	ctx, span := w.tracer.Start(ctx, "resilience.execute",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(baseAttrs...))
	w.active.Add(ctx, 1, metric.WithAttributes(baseAttrs...))

	start := time.Now()
	err := w.inner.Execute(ctx, resource, fn)
	outcome := classifyOutcome(err)
	dur := time.Since(start)

	obsAttrs := append(baseAttrs, attribute.String("resilience.outcome", outcome))
	w.calls.Add(ctx, 1, metric.WithAttributes(obsAttrs...))
	w.duration.Record(ctx, dur.Seconds(), metric.WithAttributes(obsAttrs...))
	w.active.Add(ctx, -1, metric.WithAttributes(baseAttrs...))

	span.SetAttributes(attribute.String("resilience.outcome", outcome))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
	span.End()

	if w.cfg.Level != "off" {
		w.emitLog(ctx, resource, outcome, dur, err)
	}
	return err
}

func (w *wrappedExecutor) Close() error { return w.inner.Close() }

// Refresh adopts p on the inner executor when it is [resilience.RefreshableExecutor],
// keeping the wrapper's metrics/listener intact. Returns nil (no-op) when the
// inner driver does not support hot refresh.
func (w *wrappedExecutor) Refresh(p resilience.Policy) error {
	if r, ok := w.inner.(resilience.RefreshableExecutor); ok {
		return r.Refresh(p)
	}
	return nil
}

// classifyOutcome maps an Executor's return error to a coarse outcome dimension.
// The resilience sentinels (rate-limited / circuit-open / bulkhead-full) are
// distinguished from caller timeouts and ordinary errors so a dashboard can
// separate "rejected by protection" from "downstream failed".
func classifyOutcome(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, resilience.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, resilience.ErrCircuitOpen):
		return "circuit_open"
	case errors.Is(err, resilience.ErrBulkheadFull):
		return "bulkhead_full"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "error"
	}
}

// emitLog writes one access record per Execute. A protection reject
// (rate_limited/circuit_open/bulkhead_full/timeout) is logged at Warn — it is
// a service-level degradation worth flagging; a normal downstream error is also
// Warn; success is Info.
func (w *wrappedExecutor) emitLog(ctx context.Context, resource, outcome string, dur time.Duration, err error) {
	fields := []log.Field{
		log.String("resilience.resource", resource),
		log.String("resilience.outcome", outcome),
		log.Float("duration_ms", float64(dur.Nanoseconds())/1e6),
	}
	if err != nil {
		fields = append(fields, log.Any("error", err))
		log.Warn(ctx, w.logTag, fields...)
		return
	}
	log.Info(ctx, w.logTag, fields...)
}
