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

package StarterPulsar

import (
	"context"
	"sync"

	"github.com/apache/pulsar-client-go/pulsar"
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	resilobserve "go-spring.org/observe/resilience"
)

// resilienceExecs tracks the resilience executor attached to each client, so
// GuardedSend can resolve it from the raw pulsar.Client bean and the destructor
// can Close it (releasing any background resources of a production driver).
// Only clients with resilience enabled appear here.
var resilienceExecs sync.Map // pulsar.Client -> resilience.Executor

// resilienceResources tracks the stable resource label per client so the guard
// can pass it to exec.Execute without re-deriving from Config.
var resilienceResources sync.Map // pulsar.Client -> string

// applyResilience builds an executor from the configured driver and indexes it
// by cl, unless resilience is disabled. This is the pulsar seam of
// stdlib/resilience: pulsar-client-go exposes no reject-capable middleware and
// producers are caller-created, so the executor is driven through an opt-in
// call-site guard (GuardedSend) on the synchronous Producer.Send path.
func applyResilience(c Config, cl pulsar.Client) error {
	rc := c.Resilience
	fc := c.Fault
	if !rc.Enabled && !fc.Enabled {
		return nil
	}
	rawExec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec := rawExec
	if fc.Enabled {
		exec = fault.WrapExecutor(rawExec, fault.NewInjector(fc))
	}
	exec = resilobserve.WrapExecutor(exec, "pulsar", c.Observability)
	resilienceExecs.Store(cl, exec)
	resilienceResources.Store(cl, resilience.ResourceLabel("pulsar", c.URL))
	return nil
}

// closeResilience closes and forgets the executor behind cl, if any.
func closeResilience(cl pulsar.Client) {
	if v, ok := resilienceExecs.LoadAndDelete(cl); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(cl)
}


// guard routes call through the executor attached to cl, and otherwise runs it
// inline. When resilience is disabled for the client this is a no-op
// pass-through, so enabling protection is a zero-code opt-in on the caller side.
func guard(ctx context.Context, cl pulsar.Client, call func(context.Context) error) error {
	v, ok := resilienceExecs.Load(cl)
	if !ok {
		return call(ctx)
	}
	r, _ := resilienceResources.Load(cl)
	return v.(resilience.Executor).Execute(ctx, r.(string), call)
}

// GuardedSend sends msg synchronously on producer, routed through the resilience
// executor attached to cl when Config.Resilience.Enabled is true. When
// resilience is disabled this behaves exactly like producer.Send. On rejection
// (rate-limit or open circuit) the returned error is a resilience sentinel and
// the underlying send is never invoked.
//
// The client (not the producer) is passed to resolve the executor because
// producers are caller-created and may be recreated over a client's lifetime,
// while the executor is always scoped to the client the starter created. The
// synchronous Producer.Send blocks until the broker acknowledges, which is the
// path worth protecting; the asynchronous SendAsync is intentionally untouched.
func GuardedSend(ctx context.Context, cl pulsar.Client, producer pulsar.Producer, msg *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	var id pulsar.MessageID
	err := guard(ctx, cl, func(ctx context.Context) error {
		var serr error
		id, serr = producer.Send(ctx, msg)
		return serr
	})
	if err != nil {
		return nil, err
	}
	return id, nil
}
