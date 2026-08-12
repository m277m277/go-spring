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

package StarterKafka

import (
	"context"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	resilobserve "go-spring.org/observe/resilience"
)

// resilienceExecs tracks the resilience executor attached to each client, so
// GuardedProduceSync can resolve it from the raw *kgo.Client bean and the
// destructor can Close it (releasing any background resources of a production
// driver). Only clients with resilience enabled appear here.
var resilienceExecs sync.Map // *kgo.Client -> resilience.Executor

// resilienceResources tracks the stable resource label per client so the guard
// can pass it to exec.Execute without re-deriving from Config.
var resilienceResources sync.Map // *kgo.Client -> string

// applyResilience builds an executor from the configured driver and indexes it
// by cl, unless resilience is disabled. This is the kafka (franz-go) seam of
// stdlib/resilience: franz-go's async Produce returns immediately (a record is
// handed to the internal producer and a callback fires on completion), so
// wrapping it in exec.Execute has no meaning. The synchronous ProduceSync path,
// which blocks until the broker acknowledges, is what GuardedProduceSync
// protects.
func applyResilience(c Config, cl *kgo.Client) error {
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
	exec = resilobserve.WrapExecutor(exec, "kafka", c.Observability)
	resilienceExecs.Store(cl, exec)
	resilienceResources.Store(cl, resilience.ResourceLabel("kafka", c.Brokers))
	return nil
}

// closeResilience closes and forgets the executor behind cl, if any.
func closeResilience(cl *kgo.Client) {
	if v, ok := resilienceExecs.LoadAndDelete(cl); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(cl)
}


// guard routes call through the executor attached to cl, and otherwise runs it
// inline. When resilience is disabled for the client this is a no-op
// pass-through, so enabling protection is a zero-code opt-in on the caller side.
func guard(ctx context.Context, cl *kgo.Client, call func(context.Context) error) error {
	v, ok := resilienceExecs.Load(cl)
	if !ok {
		return call(ctx)
	}
	r, _ := resilienceResources.Load(cl)
	return v.(resilience.Executor).Execute(ctx, r.(string), call)
}

// GuardedProduceSync produces recs synchronously on cl, routed through the
// resilience executor attached to cl when Config.Resilience.Enabled is true.
// When resilience is disabled this behaves exactly like cl.ProduceSync. On
// rejection (rate-limit or open circuit) the returned ProduceResults carries
// the rejection error on every record, so .FirstErr() surfaces the sentinel
// just like a real produce failure and the underlying produce is never invoked.
//
// franz-go exposes two produce APIs: Produce (async, callback on completion)
// and ProduceSync (blocks for broker ack). Only the synchronous path is
// guarded here; the async path returns immediately so an exec.Execute around
// it would be meaningless.
func GuardedProduceSync(ctx context.Context, cl *kgo.Client, recs ...*kgo.Record) kgo.ProduceResults {
	var results kgo.ProduceResults
	err := guard(ctx, cl, func(ctx context.Context) error {
		results = cl.ProduceSync(ctx, recs...)
		return results.FirstErr()
	})
	if err != nil {
		// Rejected before the produce ran: encode the rejection as a per-record
		// error so the caller's .FirstErr() surfaces it transparently.
		out := make(kgo.ProduceResults, 0, len(recs))
		for _, r := range recs {
			out = append(out, kgo.ProduceResult{Record: r, Err: err})
		}
		return out
	}
	return results
}
