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

package StarterGrpc

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// tracerName identifies spans emitted by this starter.
const tracerName = "go-spring.org/starter-grpc"

// meterName identifies metrics emitted by this starter.
const meterName = "go-spring.org/starter-grpc"

// durationBuckets are the explicit histogram bucket boundaries (in seconds) for
// RPC request duration metrics. They match the family-wide convention used by
// starter-gin (observability.go) and the shared observability kit
// (observe/observer.go).
var durationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10,
}

// --- tracing ----------------------------------------------------------------

// TracingUnaryInterceptor starts a server span for each unary gRPC request:
// it extracts the trace context from the incoming gRPC metadata (which maps to
// HTTP headers in gRPC-Web and standard gRPC), starts a span, and ends it when
// the handler returns. Errors from the handler are recorded on the span.
//
// The interceptor rides the OTel globals — when starter-otel is not imported the
// global TracerProvider and TextMapPropagator are no-ops. Importing starter-otel
// is the opt-in.
func TracingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = extractTraceContext(ctx)
		ctx, span := otel.Tracer(tracerName).Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", serviceName(info.FullMethod)),
				attribute.String("rpc.method", info.FullMethod),
			),
		)
		resp, err := handler(ctx, req)
		if err != nil {
			s, _ := status.FromError(err)
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", s.Code().String()),
			)
			span.SetStatus(codes.Error, s.Message())
			span.RecordError(err)
		}
		span.End()
		return resp, err
	}
}

// TracingStreamInterceptor starts a server span for each streaming gRPC request.
// The span wraps the entire stream lifecycle; individual messages do not get
// their own spans (that level of granularity is better served by dedicated
// instrumentation libraries).
func TracingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := extractTraceContext(ss.Context())
		ctx, span := otel.Tracer(tracerName).Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", serviceName(info.FullMethod)),
				attribute.String("rpc.method", info.FullMethod),
			),
		)
		wrapped := &wrappedServerStream{ServerStream: ss, ctx: ctx}
		err := handler(srv, wrapped)
		if err != nil {
			s, _ := status.FromError(err)
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", s.Code().String()),
			)
			span.SetStatus(codes.Error, s.Message())
			span.RecordError(err)
		}
		span.End()
		return err
	}
}

// extractTraceContext extracts the W3C trace context from gRPC metadata into the
// Go context, using the global propagator starter-otel installs. Without otel
// the global propagator is a no-op.
func extractTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(md))
}

// wrappedServerStream overrides Context() so downstream handlers and the stream
// span share the same trace context.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context { return w.ctx }

// serviceName extracts the gRPC service name from a full method path like
// "/package.Service/Method", returning "package.Service".
func serviceName(fullMethod string) string {
	if len(fullMethod) == 0 || fullMethod[0] != '/' {
		return fullMethod
	}
	s := fullMethod[1:]
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i]
		}
	}
	return s
}

// --- metrics ----------------------------------------------------------------

// MetricsUnaryInterceptor records gRPC request metrics — total count, duration,
// and in-flight gauge — through the global MeterProvider.
//
// The duration histogram follows the OTel STABLE RPC server semconv:
// "rpc.server.request.duration" (dotted). request_count and active_requests are
// kept as starter-specific extras (the stable convention only standardizes the
// duration histogram).
func MetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	meter := otel.GetMeterProvider().Meter(meterName)
	requestCount, _ := meter.Int64Counter(
		"rpc.server.request_count",
		metric.WithDescription("Number of RPC requests received"),
		metric.WithUnit("{request}"),
	)
	requestDuration, _ := meter.Float64Histogram(
		"rpc.server.request.duration",
		metric.WithDescription("Duration of RPC requests"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	requestsInFlight, _ := meter.Int64UpDownCounter(
		"rpc.server.active_requests",
		metric.WithDescription("Number of RPC requests currently in-flight"),
		metric.WithUnit("{request}"),
	)

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		attrs := metric.WithAttributes(
			attribute.String("rpc.method", info.FullMethod),
		)
		requestsInFlight.Add(ctx, 1, attrs)
		start := time.Now()

		resp, err := handler(ctx, req)

		code := status.Code(err).String()
		requestCount.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("rpc.method", info.FullMethod),
				attribute.String("rpc.grpc.status_code", code),
			),
		)
		requestDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(
				attribute.String("rpc.method", info.FullMethod),
				attribute.String("rpc.grpc.status_code", code),
			),
		)
		requestsInFlight.Add(ctx, -1, attrs)
		return resp, err
	}
}

// MetricsStreamInterceptor records gRPC stream metrics — count and duration —
// through the global MeterProvider.
//
// Streams are not covered by the stable rpc.server.request.duration semconv
// here; instead a starter-specific "rpc.server.stream.duration" (dotted) is
// emitted alongside stream_count. Both histograms use the family-wide duration
// bucket boundaries.
func MetricsStreamInterceptor() grpc.StreamServerInterceptor {
	meter := otel.GetMeterProvider().Meter(meterName)
	streamCount, _ := meter.Int64Counter(
		"rpc.server.stream_count",
		metric.WithDescription("Number of RPC stream requests received"),
		metric.WithUnit("{request}"),
	)
	streamDuration, _ := meter.Float64Histogram(
		"rpc.server.stream.duration",
		metric.WithDescription("Duration of RPC stream requests"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		code := status.Code(err).String()
		streamCount.Add(ss.Context(), 1,
			metric.WithAttributes(
				attribute.String("rpc.method", info.FullMethod),
				attribute.String("rpc.grpc.status_code", code),
			),
		)
		streamDuration.Record(ss.Context(), time.Since(start).Seconds(),
			metric.WithAttributes(
				attribute.String("rpc.method", info.FullMethod),
				attribute.String("rpc.grpc.status_code", code),
			),
		)
		return err
	}
}
