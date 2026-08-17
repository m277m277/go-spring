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

// trace.go is the notification-adjacent tracing pattern taken from
// starter-mail: small call-site span helpers riding the OTel globals that
// starter-otel installs. Without starter-otel they are no-ops and touch no
// bytes on the wire.
package StarterWebhook

import (
	"context"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies spans emitted by this starter.
const tracerName = "go-spring.org/starter-webhook"

// StartSendSpan starts a producer span for one webhook delivery. Call it
// right before the POST and end the returned span once delivery settles:
//
//	ctx, span := StarterWebhook.StartSendSpan(ctx, "dingtalk", url)
//	err := notifier.post(ctx, url, body)
//	StarterWebhook.EndSpan(span, err)
func StartSendSpan(ctx context.Context, channel, endpoint string) (context.Context, trace.Span) {
	tracer := otel.GetTracerProvider().Tracer(tracerName)
	return tracer.Start(ctx, "webhook.send",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "webhook"),
			attribute.String("webhook.channel", channel),
			attribute.String("webhook.destination.host", hostOf(endpoint)),
		),
	)
}

// EndSpan records err (if any) on span and ends it. It is a small convenience
// so callers do not have to import the OTel codes package themselves.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// startSend is the internal Send path of StartSendSpan.
func startSend(ctx context.Context, channel, endpoint string) (context.Context, trace.Span) {
	return StartSendSpan(ctx, channel, endpoint)
}

// hostOf extracts scheme://host for the span attribute, keeping full
// endpoints out of telemetry.
func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}
