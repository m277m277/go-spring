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
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	resilobserve "go-spring.org/observe/resilience"
)

// resilienceExecs tracks the resilience executor attached to each client, so
// GuardedPublish can resolve it from the raw mqtt.Client bean and the destructor
// can Close it (releasing any background resources of a production driver).
// Only clients with resilience enabled appear here.
var resilienceExecs sync.Map // mqtt.Client -> resilience.Executor

// resilienceResources tracks the stable resource label per client so the guard
// can pass it to exec.Execute without re-deriving from Config.
var resilienceResources sync.Map // mqtt.Client -> string

// applyResilience builds an executor and indexes it by cl. This is the mqtt seam
// of resilience. paho's Publish hands the message to the client's internal
// outbound queue and returns a Token; the caller then blocks on token.Wait().
// For QoS 0 Wait() returns once the packet is written; for QoS 1/2 it blocks
// until the PUBACK/PUBCOMP. Because paho manages its own queueing and reconnect,
// the executor here is intentionally minimal — rate limiting the publish rate
// and short-circuiting (circuit breaker) when the broker is unhealthy. It is
// driven through an opt-in call-site guard (GuardedPublish).
//
// The executor is resolved through the neutral [resilience.ExecutorFor] seam,
// which starter-govern backs with the governance center — so this function has
// zero coupling to cloud/govern. When governance is off, ExecutorFor yields a
// transparent no-op executor; fault wraps it when enabled.
func applyResilience(c Config, cl mqtt.Client, resource string) error {
	fc := c.Fault
	exec := resilience.ExecutorFor(resource)
	if fc.Enabled {
		exec = fault.WrapExecutor(exec, fault.NewInjector(fc))
	}
	exec = resilobserve.WrapExecutor(exec, "mqtt", c.Observability)
	resilienceExecs.Store(cl, exec)
	resilienceResources.Store(cl, resource)
	return nil
}

// closeResilience closes and forgets the executor behind cl, if any.
func closeResilience(cl mqtt.Client) {
	if v, ok := resilienceExecs.LoadAndDelete(cl); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(cl)
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
// resilience executor attached to cl when governance is enabled.
// When governance is disabled this behaves exactly like a plain Client.Publish
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
