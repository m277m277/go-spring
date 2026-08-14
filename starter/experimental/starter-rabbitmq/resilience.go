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

package StarterRabbitMQ

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"go-spring.org/cloud/governance/fault"
	"go-spring.org/cloud/governance/resilience"
	resilobserve "go-spring.org/cloud/observe/resilience"
)

// resilienceExecs tracks the resilience executor attached to each connection,
// so GuardedPublish can resolve it from the raw *amqp.Connection bean and the
// destructor can Close it (releasing any background resources of a production
// driver). Only connections with resilience enabled appear here.
var resilienceExecs sync.Map // *amqp.Connection -> resilience.Executor

// resilienceResources tracks the stable resource label per connection so the
// guard can pass it to exec.Execute without re-deriving from Config.
var resilienceResources sync.Map // *amqp.Connection -> string

// applyResilience builds an executor and indexes it by conn. This is the
// rabbitmq seam of resilience: amqp091 exposes no reject-capable middleware and
// channels are caller-created from the connection, so the executor is driven
// through an opt-in call-site guard (GuardedPublish) rather than a transparent
// interceptor.
//
// The executor is resolved through the neutral [resilience.ExecutorFor] seam,
// which starter-govern backs with the governance center — so this function has
// zero coupling to cloud/governance. When governance is off, ExecutorFor yields a
// transparent no-op executor; fault wraps it when enabled.
func applyResilience(c Config, conn *amqp.Connection, resource string) error {
	exec := fault.WrapExecutor(resilience.ExecutorFor(resource), fault.InjectorFor())
	exec = resilobserve.WrapExecutor(exec, "rabbitmq", c.Observability)
	resilienceExecs.Store(conn, exec)
	resilienceResources.Store(conn, resource)
	return nil
}

// closeResilience closes and forgets the executor behind conn, if any.
func closeResilience(conn *amqp.Connection) {
	if v, ok := resilienceExecs.LoadAndDelete(conn); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(conn)
}

// guard routes call through the executor attached to conn, and otherwise runs it
// inline. When resilience is disabled for the connection this is a no-op
// pass-through, so enabling protection is a zero-code opt-in on the caller side.
func guard(ctx context.Context, conn *amqp.Connection, call func(context.Context) error) error {
	v, ok := resilienceExecs.Load(conn)
	if !ok {
		return call(ctx)
	}
	r, _ := resilienceResources.Load(conn)
	return v.(resilience.Executor).Execute(ctx, r.(string), call)
}

// GuardedPublish publishes pub to exchange/routingKey on ch, routed through the
// resilience executor attached to conn when governance is enabled.
// When governance is disabled this behaves exactly like ch.PublishWithContext.
// On rejection (rate-limit or open circuit) the returned error is a resilience
// sentinel and the underlying publish is never invoked.
//
// The connection (not the channel) is passed to resolve the executor because a
// channel may outlive the connection bean in some patterns, while the executor
// is always scoped to the connection the starter created.
func GuardedPublish(ctx context.Context, conn *amqp.Connection, ch *amqp.Channel, exchange, key string, mandatory, immediate bool, pub amqp.Publishing) error {
	return guard(ctx, conn, func(ctx context.Context) error {
		return ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, pub)
	})
}
