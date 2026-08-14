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

package StarterMQTT

import (
	"context"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	observe "go-spring.org/cloud/observe"
)

// MQTT observability is driven by these kit-backed helpers rather than a
// transparent client wrapper, for two reasons:
//
//  1. paho.mqtt.golang (v1) ships no OTel instrumentation, and Publish returns
//     an async Token whose error/timing is not known at the call site — a
//     transparent wrapper would have to wrap the Token too, which is fragile.
//
//  2. MQTT 3.1.1 (what paho v1 speaks) carries no message properties, so W3C
//     trace context cannot propagate across the broker: publish and consume
//     spans are independent traces. That is an inherent protocol limitation,
//     not an instrumentation gap.
//
// The helpers emit the full three-signal trio (span + duration/in-flight metric
// + access log) via the shared observe kit, riding the OTel globals starter-otel
// installs. Call them around Publish and inside the Subscribe callback.

var (
	pubObs = observe.NewProducer("mqtt", observe.ObserveConfig{Level: observe.DefaultBrief})
	subObs = observe.NewConsumer("mqtt", observe.ObserveConfig{Level: observe.DefaultBrief})
)

// StartPublishSpan opens a producer observation for a publish to topic. Call
// right before client.Publish and End the returned span once the token resolves:
//
//	ctx, sp := StarterMQTT.StartPublishSpan(ctx, "sensors/temp")
//	tok := client.Publish("sensors/temp", qos, false, payload)
//	_ = tok.Wait()
//	StarterMQTT.EndSpan(sp, tok.Error())
func StartPublishSpan(ctx context.Context, topic string) (context.Context, *observe.Span) {
	return pubObs.Start(ctx, "publish", topic)
}

// StartConsumeSpan opens a consumer observation for an inbound message. Call at
// the top of a subscription callback and End once handling finishes:
//
//	sub, _ := client.Subscribe("sensors/temp", qos, func(c mqtt.Client, m mqtt.Message) {
//	    ctx, sp := StarterMQTT.StartConsumeSpan(ctx, m)
//	    err := handle(ctx, m)
//	    StarterMQTT.EndSpan(sp, err)
//	})
func StartConsumeSpan(ctx context.Context, msg mqtt.Message) (context.Context, *observe.Span) {
	return subObs.Start(ctx, "consume", msg.Topic())
}

// EndSpan records err (if any) on the span and ends it.
func EndSpan(span *observe.Span, err error) {
	span.End(err)
}
