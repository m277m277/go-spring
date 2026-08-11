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

package StarterKafkaSarama

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
	resilobserve "go-spring.org/observe-resilience"
	"go-spring.org/spring/experimental/cloud/resilience"
)

// resilienceExecs tracks the resilience executor attached to each client, so
// WrapSyncProducer can resolve it from the raw sarama.Client bean and the
// destructor can Close it (releasing any background resources of a production
// driver). Only clients with resilience enabled appear here.
var resilienceExecs sync.Map // sarama.Client -> resilience.Executor

// resilienceResources tracks the stable resource label per client so the
// wrapped SyncProducer can pass it to exec.Execute without re-deriving from
// Config (which the wrapper no longer holds at call time).
var resilienceResources sync.Map // sarama.Client -> string

// applyResilience builds an executor from the configured driver and indexes it
// by client, unless resilience is disabled. This is the kafka-sarama seam of
// stdlib/resilience: sarama exposes no reject-capable middleware, so the
// executor is driven through a transparent SyncProducer wrapper (see
// WrapSyncProducer) that callers opt into once after creating their producer.
func applyResilience(c Config, client sarama.Client) error {
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
	// Wrap so breaker trips / rejects / retries emit span + counter + histogram
	// + access log (the resilience core emits none). nil-safe, no-op without
	// starter-otel.
	exec = resilobserve.WrapExecutor(exec, "kafka", c.Observability)
	resilienceExecs.Store(client, exec)
	resilienceResources.Store(client, resourceLabel(c))
	return nil
}

// closeResilience closes and forgets the executor behind client, if any.
func closeResilience(client sarama.Client) {
	if v, ok := resilienceExecs.LoadAndDelete(client); ok {
		_ = v.(resilience.Executor).Close()
	}
	resilienceResources.Delete(client)
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// client, so limiter and breaker state is scoped per Kafka cluster (by broker
// seed list) rather than per message. Uses the shared [resilience.ResourceLabel]
// helper.
func resourceLabel(c Config) string {
	return resilience.ResourceLabel("kafka", c.Brokers)
}

// executorFor loads the executor and resource label attached to client. Returns
// (nil, "") when resilience is disabled for that client, so the wrapper falls
// back to a direct call.
func executorFor(client sarama.Client) (resilience.Executor, string) {
	v, ok := resilienceExecs.Load(client)
	if !ok {
		return nil, ""
	}
	r, _ := resilienceResources.Load(client)
	return v.(resilience.Executor), r.(string)
}

// WrapSyncProducer returns a sarama.SyncProducer that routes SendMessage and
// SendMessages through the resilience executor attached to cl when
// Config.Resilience.Enabled is true. When resilience is disabled (or cl was
// created without it) p is returned unchanged, so wrapping is a zero-risk
// opt-in: callers always wrap unconditionally.
//
// Only the synchronous send paths are guarded; the transaction and Close methods
// delegate directly, as does TxnStatus/IsTransactional. SendMessage takes no
// context.Context (sarama's API is context-free), so a background context is
// used — pass a per-call deadline via AttemptTimeout/MaxDuration in
// [resilience.Config] when a bound matters.
//
//	prod, _ := sarama.NewSyncProducerFromClient(cl)
//	prod = StarterKafkaSarama.WrapSyncProducer(cl, prod)
//	_, _, err := prod.SendMessage(msg) // now rate-limited / circuit-guarded
func WrapSyncProducer(cl sarama.Client, p sarama.SyncProducer) sarama.SyncProducer {
	exec, resource := executorFor(cl)
	if exec == nil {
		return p
	}
	return &guardedSyncProducer{p: p, exec: exec, resource: resource}
}

// guardedSyncProducer is a transparent sarama.SyncProducer wrapper that drives
// the synchronous send methods through the resilience executor. All other
// methods (Close, transaction lifecycle, status queries) delegate to the inner
// producer unchanged — they are control-plane, not the protected data path.
type guardedSyncProducer struct {
	p        sarama.SyncProducer
	exec     resilience.Executor
	resource string
}

var _ sarama.SyncProducer = (*guardedSyncProducer)(nil)

func (g *guardedSyncProducer) SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error) {
	err = g.exec.Execute(context.Background(), g.resource, func(context.Context) error {
		var perr error
		partition, offset, perr = g.p.SendMessage(msg)
		return perr
	})
	if err != nil {
		return -1, -1, err
	}
	return partition, offset, nil
}

func (g *guardedSyncProducer) SendMessages(msgs []*sarama.ProducerMessage) error {
	return g.exec.Execute(context.Background(), g.resource, func(context.Context) error {
		return g.p.SendMessages(msgs)
	})
}

func (g *guardedSyncProducer) Close() error                                          { return g.p.Close() }
func (g *guardedSyncProducer) TxnStatus() sarama.ProducerTxnStatusFlag              { return g.p.TxnStatus() }
func (g *guardedSyncProducer) IsTransactional() bool                                 { return g.p.IsTransactional() }
func (g *guardedSyncProducer) BeginTxn() error                                       { return g.p.BeginTxn() }
func (g *guardedSyncProducer) CommitTxn() error                                      { return g.p.CommitTxn() }
func (g *guardedSyncProducer) AbortTxn() error                                       { return g.p.AbortTxn() }
func (g *guardedSyncProducer) AddOffsetsToTxn(o map[string][]*sarama.PartitionOffsetMetadata, gid string) error {
	return g.p.AddOffsetsToTxn(o, gid)
}
func (g *guardedSyncProducer) AddMessageToTxn(msg *sarama.ConsumerMessage, gid string, metadata *string) error {
	return g.p.AddMessageToTxn(msg, gid, metadata)
}
