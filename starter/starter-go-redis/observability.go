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

package StarterGoRedis

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
	observe "go-spring.org/cloud/observe"
)

// applyObservability attaches an access-log Hook to client. go-redis already
// emits trace+metric via redisotel (InstrumentTracing/InstrumentMetrics in
// instrument()), so the Hook's Observer is built with WithoutTraceAndMetric:
// the kit only fills the access-log gap, avoiding duplicate spans/metrics. The
// log rides the caller's ctx, so it picks up redisotel's span for trace_id
// correlation. When ObserveConfig.Level is "off" the Observer emits nothing, so the
// Hook is a no-op pass-through.
func applyObservability(cfg observe.ObserveConfig, client redis.UniversalClient) {
	obs := observe.NewDB("redis", cfg, observe.WithoutTraceAndMetric())
	client.AddHook(&observeHook{obs: obs})
}

// observeHook emits a per-command access log around every Redis command and
// pipeline. It does not emit spans or metrics (those come from redisotel); it
// only drives the Observer's log path.
type observeHook struct{ obs *observe.Observer }

var _ redis.Hook = (*observeHook)(nil)

func (h *observeHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *observeHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		_, sp := h.obs.Start(ctx, cmd.FullName(), "")
		err := next(ctx, cmd)
		sp.End(nilAsSuccess(err))
		return err
	}
}

func (h *observeHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		_, sp := h.obs.Start(ctx, "pipeline", "")
		err := next(ctx, cmds)
		sp.End(nilAsSuccess(err))
		return err
	}
}

// nilAsSuccess treats redis.Nil (a cache miss / "key not found") as success so
// it does not mark the access log entry as an error — mirroring applyResilience.
func nilAsSuccess(err error) error {
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}
