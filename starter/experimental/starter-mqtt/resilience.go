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
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	resilobserve "go-spring.org/observe-resilience"
	"go-spring.org/spring/experimental/cloud/resilience"
)

// resilienceExecs tracks the resilience executor attached to each client, so
// GuardedPublish can resolve it from the raw mqtt.Client bean and the destructor
// can Close it (releasing any background resources of a production driver).
// Only clients with resilience enabled appear here.
var resilienceExecs sync.Map // mqtt.Client -> resilience.Executor

// resilienceResources tracks the stable resource label per client so the guard
// can pass it to exec.Execute without re-deriving from Config.
var resilienceResources sync.Map // mqtt.Client -> string

// applyResilience builds an executor from the configured driver and indexes it
// by cl, unless resilience is disabled. This is the mqtt seam of
// stdlib/resilience. paho's Publish hands the message to the client's internal
// outbound queue and returns a Token; the caller then blocks on token.Wait().
// For QoS 0 Wait() returns once the packet is written; for QoS 1/2 it blocks
// until the PUBACK/PUBCOMP. Because paho manages its own queueing and reconnect,
// the executor here is intentionally minimal — rate limiting the publish rate
// and short-circuiting (circuit breaker) when the broker is unhealthy. It is
// driven through an opt-in call-site guard (GuardedPublish).
func applyResilience(c Config, cl mqtt.Client) error {
	if !c.Resilience.Enabled {
		return nil
	}
	drv, err := resilience.MustGetDriver(c.Resilience.Driver)
	if err != nil {
		return err
	}
	exec, err := drv.NewExecutor(c.Resilience.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "mqtt", c.Observability)
	resilienceExecs.Store(cl, exec)
	resilienceResources.Store(cl, resourceLabel(c))
	return nil
}

// closeResilience closes and forgets the executor behind cl, if any.
func closeResilience(cl mqtt.Client) {
	if v, ok := resilienceExecs.LoadAndDelete(cl); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(cl)
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// client, so limiter and breaker state is scoped per broker (by broker address)
// rather than per message. Uses the shared [resilience.ResourceLabel] helper.
func resourceLabel(c Config) string {
	return resilience.ResourceLabel("mqtt", c.Broker)
}

// guard routes call through the executor attached to cl, and otherwise runs it
// inline. When resilience is disabled for the client this is a no-op
// pass-through, so enabling protection is a zero-code opt-in on the caller side.
func guard(ctx context.Context, cl mqtt.Client, call func(context.Context) error) error {
	v, ok := resilienceExecs.Load(cl)
	if !ok {
		return call(ctx)
	}
	r, _ := resilienceResources.Load(cl)
	return v.(resilience.Executor).Execute(ctx, r.(string), call)
}

// GuardedPublish publishes payload to topic at qos, routed through the
// resilience executor attached to cl when Config.Resilience.Enabled is true.
// When resilience is disabled this behaves exactly like a plain Client.Publish
// followed by token.Wait(). On rejection (rate-limit or open circuit) the
// returned error is a resilience sentinel and the underlying publish is never
// invoked.
//
// retained controls broker-side retention, matching the paho Publish signature.
// The function blocks until paho acknowledges the outbound handoff (immediately
// at QoS 0, after a PUBACK/PUBCOMP at QoS 1/2).
func GuardedPublish(ctx context.Context, cl mqtt.Client, topic string, qos byte, retained bool, payload interface{}) error {
	return guard(ctx, cl, func(context.Context) error {
		token := cl.Publish(topic, qos, retained, payload)
		token.Wait()
		return token.Error()
	})
}
