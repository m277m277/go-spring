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

// Package lockobserve is the shared distributed-lock instrumentation adapter
// for the go-spring observability story. It wraps any [lock.Locker] so Acquire
// and TryAcquire emit an OTel client span labelled with the lock backend's
// [lock.system] semantic-convention value (e.g. "redis", "etcd", "consul",
// "k8s") — the same trace signal every lock starter previously emitted.
//
// It lives in its own module (rather than copy-pasted into each lock starter,
// or inside the otel-free spring core that defines [lock.Locker]) so the four
// lock starters share one implementation instead of duplicating a ~70-line
// wrapper each, differing only in the system label. A starter installs it with
// its backend's system value:
//
//	locker = lockobserve.WrapLocker("redis", inner)
package lockobserve

import (
	"context"

	"go-spring.org/cloud/experimental/lock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies spans emitted by this adapter, following the
// convention used by the other shared observe adapters.
const tracerName = "go-spring.org/observe/lock"

// WrapLocker returns a [lock.Locker] that wraps Acquire and TryAcquire with
// OTel client spans labelled lock.system=system and lock.key=<key>. When
// starter-otel is not imported the global TracerProvider is a no-op, so the
// wrapper adds negligible overhead and changes no behaviour.
func WrapLocker(system string, inner lock.Locker) lock.Locker {
	return &observedLocker{system: system, inner: inner}
}

type observedLocker struct {
	system string
	inner  lock.Locker
}

func (l *observedLocker) Acquire(ctx context.Context, key string, opts ...lock.Option) (lock.Lock, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "lock.acquire",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("lock.key", key),
			attribute.String("lock.system", l.system),
		),
	)
	held, err := l.inner.Acquire(ctx, key, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
	span.End()
	return held, err
}

func (l *observedLocker) TryAcquire(ctx context.Context, key string, opts ...lock.Option) (lock.Lock, bool, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "lock.try_acquire",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("lock.key", key),
			attribute.String("lock.system", l.system),
		),
	)
	held, ok, err := l.inner.TryAcquire(ctx, key, opts...)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
	if !ok {
		span.SetAttributes(attribute.Bool("lock.acquired", false))
	}
	span.End()
	return held, ok, err
}

func (l *observedLocker) Close() error { return l.inner.Close() }
