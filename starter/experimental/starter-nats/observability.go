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

package StarterNats

import (
	"context"

	"github.com/nats-io/nats.go"
	observe "go-spring.org/cloud/observe"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Publish-side instrumentation is transparent: Conn.PublishMsg (below) wraps
// every publish with a producer span + duration/in-flight metric + access log,
// and injects the W3C trace context into msg.Header so subscribers continue the
// trace. Both the messaging.Binder publisher and direct Conn.PublishMsg callers
// flow through it.
//
// Subscribe-side instrumentation lives in the binder callback (binder.go): the
// nats.MsgHandler signature (func(*nats.Msg), no context) means the consume span
// must wrap the handler at the point that already bridges *nats.Msg to a
// context-bearing handler — i.e. the binder. Direct Conn.Subscribe callers are
// not instrumented (documented gap, same as the JetStream surface); use the
// messaging.Binder for traced consume.

// injectW3C inserts the current trace context into msg.Header so the receiver
// can continue the trace across the broker.
func injectW3C(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(msg.Header))
}

// extractW3C pulls the upstream trace context out of msg.Header. Returns the
// input ctx unchanged when no context was propagated.
func extractW3C(ctx context.Context, msg *nats.Msg) context.Context {
	if len(msg.Header) == 0 {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.HeaderCarrier(msg.Header))
}

// PublishMsg overrides the embedded *nats.Conn.PublishMsg so every publish flows
// through the observe kit. When pubObs is nil (observability level "off" leaves
// the observer present but a no-op; nil only when the field was never set) it
// delegates unchanged. nats.PublishMsg carries no context, so the producer span
// is started from context.Background; cross-service linkage still works because
// the span's context is injected into msg.Header for the consumer to extract.
func (c *Conn) PublishMsg(msg *nats.Msg) error {
	if c.pubObs == nil {
		return c.Conn.PublishMsg(msg)
	}
	ctx, sp := c.pubObs.Start(context.Background(), "publish", msg.Subject)
	injectW3C(ctx, msg)
	err := c.Conn.PublishMsg(msg)
	sp.End(err)
	return err
}

// startConsume is the subscribe-side hook the binder callback calls: it extracts
// the upstream trace, opens a consumer span + metric + log, and returns a handle
// whose End records the outcome.
func (c *Conn) startConsume(ctx context.Context, subject string, msg *nats.Msg) (context.Context, *observe.Span) {
	if c.subObs == nil {
		return ctx, nil
	}
	ctx2, sp := c.subObs.Start(extractW3C(ctx, msg), "consume", subject)
	return ctx2, sp
}

// --- low-level manual helpers (kept for the example programs and for apps that
// want explicit span control outside the messaging.Binder) -------------------
//
// The auto path above (Conn.PublishMsg + binder) is preferred. These helpers are
// a thin otel-direct escape hatch: they emit a span only (no metric/log) and do
// not require an Observer, so they work with a bare *nats.Conn the app holds.

const tracerName = "go-spring.org/starter-nats"

// StartPublishSpan starts a producer span for msg and injects the current W3C
// trace context into msg.Header. Kept for backward compatibility / manual use.
func StartPublishSpan(ctx context.Context, msg *nats.Msg) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "nats.publish "+msg.Subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", msg.Subject),
			attribute.String("messaging.operation", "publish"),
		),
	)
	injectW3C(ctx, msg)
	return ctx, span
}

// StartConsumeSpan extracts the upstream trace from msg.Header and starts a
// consumer span as its child. Kept for backward compatibility / manual use.
func StartConsumeSpan(ctx context.Context, msg *nats.Msg) (context.Context, trace.Span) {
	ctx = extractW3C(ctx, msg)
	ctx, span := otel.Tracer(tracerName).Start(ctx, "nats.consume "+msg.Subject,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", msg.Subject),
			attribute.String("messaging.operation", "receive"),
		),
	)
	return ctx, span
}

// EndSpan records err (if any) on span and ends it.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
