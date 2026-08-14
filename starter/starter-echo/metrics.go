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

package StarterEcho

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meterName identifies metrics emitted by this starter.
const meterName = "go-spring.org/starter-echo"

// --- metrics ----------------------------------------------------------------

// metricsMiddleware records HTTP request metrics — duration and in-flight gauge
// — through the global MeterProvider that starter-otel installs. When starter-otel
// is not imported the global MeterProvider is a no-op, so this costs almost nothing.
// The middleware is on by default; importing starter-otel activates it.
//
// Note: the per-request count is already provided by the duration histogram's
// _count aggregation, so no separate counter is declared here (matches the OTel
// stable HTTP semconv and starter-gin's metric set).
func metricsMiddleware() echo.MiddlewareFunc {
	meter := otel.GetMeterProvider().Meter(meterName)

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

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			attrs := metric.WithAttributes(
				attribute.String("http.request.method", c.Request().Method),
				attribute.String("http.route", c.Path()),
			)

			requestsInFlight.Add(c.Request().Context(), 1, attrs)
			start := time.Now()

			err := next(c)

			status := strconv.Itoa(c.Response().Status)
			requestDuration.Record(c.Request().Context(), time.Since(start).Seconds(),
				metric.WithAttributes(
					attribute.String("http.request.method", c.Request().Method),
					attribute.String("http.route", c.Path()),
					attribute.String("http.response.status_code", status),
				),
			)
			requestsInFlight.Add(c.Request().Context(), -1, attrs)
			return err
		}
	}
}
