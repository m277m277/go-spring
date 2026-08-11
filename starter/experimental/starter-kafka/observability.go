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
	observe "go-spring.org/observe"
)

// newObserveHook builds a kgo hook that emits a per-message access log through
// the observe kit. kotel already provides producer/consumer spans and client
// metrics, so this hook is log-only (WithoutTraceAndMetric): it fills the
// access-log gap without duplicating spans or metrics. The produce path pairs
// OnProduceRecordBuffered (start) with OnProduceRecordUnbuffered (end) so the
// log carries accurate duration; consume has no paired start hook, so it emits
// a duration-less record (the metric covers consume latency).
func newObserveHook(cfg observe.LogConfig) kgo.Hook {
	return &observeHook{
		pubObs: observe.NewProducer("kafka", cfg, observe.WithoutTraceAndMetric()),
		subObs: observe.NewConsumer("kafka", cfg, observe.WithoutTraceAndMetric()),
	}
}

type observeHook struct {
	pubObs *observe.Observer
	subObs *observe.Observer
	spans  sync.Map // *kgo.Record -> *observe.Span (in-flight produces)
}

// OnProduceRecordBuffered opens a producer observation when a record is queued.
func (h *observeHook) OnProduceRecordBuffered(r *kgo.Record) {
	_, sp := h.pubObs.Start(context.Background(), "publish", r.Topic)
	h.spans.Store(r, sp)
}

// OnProduceRecordUnbuffered closes the producer observation when the record is
// acknowledged (or fails), recording the outcome and the buffered→unbuffered
// duration.
func (h *observeHook) OnProduceRecordUnbuffered(r *kgo.Record, err error) {
	if v, ok := h.spans.LoadAndDelete(r); ok {
		v.(*observe.Span).End(err)
	}
}

// OnFetchRecordRead emits a consume access record per message read.
func (h *observeHook) OnFetchRecordRead(r *kgo.Record) {
	_, sp := h.subObs.Start(context.Background(), "consume", r.Topic)
	sp.End(nil)
}
