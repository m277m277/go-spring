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

package StarterHertz

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies spans emitted by this starter.
const tracerName = "go-spring.org/starter-hertz"

// meterName identifies metrics emitted by this starter.
const meterName = "go-spring.org/starter-hertz"

// --- tracing ----------------------------------------------------------------

// tracingMiddleware starts a server span for each inbound request: it extracts
// any trace context the caller propagated in headers and starts a span named
// "HTTP <method>". When the request completes, status code and path are stamped
// as span attributes; 5xx responses mark the span as errored.
//
// The middleware rides the OTel globals — when starter-otel is not imported the
// global TracerProvider and TextMapPropagator are no-ops, so this costs almost
// nothing. The middleware is on by default; importing starter-otel activates it.
func tracingMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// Hertz stores headers in a []args.Header but the OTel Propagation
		// API needs http.Header. Build an adapter.
		hdr := make(http.Header)
		c.Request.Header.VisitAll(func(key, value []byte) {
			hdr.Add(string(key), string(value))
		})

		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(hdr))
		ctx, span := otel.Tracer(tracerName).Start(ctx, "HTTP "+string(c.Request.Method()),
			trace.WithSpanKind(trace.SpanKindServer),
		)

		span.SetAttributes(
			attribute.String("http.request.method", string(c.Request.Method())),
			attribute.String("url.path", string(c.Request.URI().Path())),
			attribute.String("server.address", string(c.Request.Host())),
		)

		c.Next(ctx)

		span.SetAttributes(
			attribute.Int("http.response.status_code", c.Response.StatusCode()),
		)
		if status := c.Response.StatusCode(); status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		span.End()
	}
}

// --- metrics ----------------------------------------------------------------

// metricsMiddleware records HTTP request metrics — total count, duration, and
// in-flight gauge — through the global MeterProvider that starter-otel installs.
// When starter-otel is not imported the global MeterProvider is a no-op, so this
// costs almost nothing. The middleware is on by default; importing starter-otel activates it.
func metricsMiddleware() app.HandlerFunc {
	meter := otel.GetMeterProvider().Meter(meterName)

	requestCount, _ := meter.Int64Counter(
		"http.server.request_count",
		metric.WithDescription("Number of HTTP requests received"),
		metric.WithUnit("{request}"),
	)
	requestDuration, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithUnit("s"),
		// OTel HTTP semconv recommended buckets (seconds).
		metric.WithExplicitBucketBoundaries(
			0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10,
		),
	)
	requestsInFlight, _ := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of HTTP requests currently in-flight"),
		metric.WithUnit("{request}"),
	)

	return func(ctx context.Context, c *app.RequestContext) {
		method := string(c.Request.Method())

		attrs := metric.WithAttributes(
			attribute.String("http.request.method", method),
			attribute.String("http.route", string(c.Request.URI().Path())),
		)

		requestsInFlight.Add(ctx, 1, attrs)
		start := time.Now()

		c.Next(ctx)

		status := strconv.Itoa(c.Response.StatusCode())
		requestCount.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("http.request.method", method),
				attribute.String("http.route", string(c.Request.URI().Path())),
				attribute.String("http.response.status_code", status),
			),
		)
		requestDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(
				attribute.String("http.request.method", method),
				attribute.String("http.route", string(c.Request.URI().Path())),
				attribute.String("http.response.status_code", status),
			),
		)
		requestsInFlight.Add(ctx, -1, attrs)
	}
}
