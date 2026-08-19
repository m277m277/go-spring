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

// Package StarterOTel defines go-spring's unified, framework-level
// observability layer. It builds the shared OTel TracerProvider and
// MeterProvider from ${spring.observability} and installs them as the process
// globals so any instrumented component (starter-gorm-*, ...) that reads
// otel.GetTracerProvider()/GetMeterProvider() is wired up automatically -
// configure once, no per-component adaptation.
package StarterOTel

import (
	"context"
	"sync"

	"go-spring.org/cloud/actuator/endpoint"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	"go-spring.org/starter-otel/metric"
	"go-spring.org/starter-otel/trace"
	"go-spring.org/stdlib/flatten"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
)

var (
	// starterTag identifies logs emitted by the otel starter.
	starterTag = log.RegisterAppTag("starter_otel", "")

	// runtimeOnce guards runtimemetrics.Start, which is not idempotent: the OTel
	// contrib runtime instrumentation registers fresh async callbacks on the
	// MeterProvider each call, so a second invocation (e.g. across gs.RunTest
	// re-runs against the same provider) would create duplicate instruments.
	// The first error is sticky so a later re-run does not silently lose the
	// original failure.
	runtimeOnce sync.Once
	runtimeErr  error
)

func init() {
	// A nil condition means the module always runs when the starter is imported;
	// importing starter-otel activates the OTel SDK. The actual on/off is decided inside
	// setup from ${spring.observability.enable} (default true). This must be a
	// gs.Module, not a plain bean: its body executes during applyModules in the
	// RefreshPrepare phase, i.e. BEFORE any bean is instantiated. Setting the
	// OTel globals here therefore guarantees they are live before component beans
	// (e.g. a gorm client calling db.Use) are constructed. Building the providers
	// lazily inside a bean constructor would break that ordering.
	gs.Module(nil, setup)
}

// setup binds ${spring.observability} and builds the shared trace/metrics
// resource, then delegates each pillar to setupTrace / setupMetrics. Returning
// early on Enable=false leaves the globals as the SDK's no-op providers, so an
// imported-but-disabled starter has no effect.
func setup(r gs.BeanProvider, p flatten.Storage) error {
	var cfg Config
	if err := conf.Bind(p, &cfg, "${spring.observability}"); err != nil {
		return err
	}
	if !cfg.Enable {
		log.Infof(context.Background(), starterTag, "observability disabled; skipping OTel setup")
		return nil
	}

	log.Debugf(context.Background(), starterTag, "setting up OTel with service=%s trace_enable=%v metrics_enable=%v", cfg.ServiceName, cfg.Trace.Enable, cfg.Metrics.Enable)

	res, err := trace.NewResource(cfg.ServiceName)
	if err != nil {
		return err
	}

	if err := setupTrace(cfg.Trace, res); err != nil {
		return err
	}
	if err := setupMetrics(r, cfg.Metrics, res); err != nil {
		return err
	}
	return nil
}

// setupTrace builds the TracerProvider and propagator from the trace config,
// installs them as OTel globals, and registers the provider as a
// process-global stopper (flushed at shutdown via gs.RegisterStopper). Once
// the global propagator is set, every instrumented client (otelhttp,
// otelgrpc, ...) propagates the active trace context on outbound requests
// automatically - no per-component wiring. It is a no-op when tracing is
// disabled or exporter is "none".
func setupTrace(cfg trace.TraceConfig, res *resource.Resource) error {
	if !cfg.Enable || cfg.Exporter == "none" {
		return nil
	}

	tp, err := trace.NewTracerProvider(cfg, res)
	if err != nil {
		return err
	}
	prop, err := trace.NewPropagator(cfg.Propagator)
	if err != nil {
		return err
	}
	otel.SetTracerProvider(tp)
	if prop != nil {
		otel.SetTextMapPropagator(prop)
	}
	// The provider is a process-global resource, so register it as a global
	// stopper (not a bean destroyer) to flush buffered spans at shutdown.
	gs.RegisterStopper("otel-trace", tp.Shutdown)

	log.Infof(context.Background(), starterTag, "trace provider initialized exporter=%s propagator=%s", cfg.Exporter, cfg.Propagator)
	return nil
}

// setupMetrics builds the MeterProvider from the metrics config, installs it as
// the OTel global, registers it (and, for the pull-based Prometheus exporter, its
// dedicated scrape server) as process-global stoppers via gs.RegisterStopper,
// contributes the scrape handler as an actuator endpoint, and feeds Go runtime
// metrics into the provider when enabled. It is a no-op when metrics is disabled
// or exporter is "none".
func setupMetrics(r gs.BeanProvider, cfg metric.MetricsConfig, res *resource.Resource) error {
	if !cfg.Enable || cfg.Exporter == "none" {
		return nil
	}

	mp, ps, err := metric.NewMeterProvider(cfg, res)
	if err != nil {
		return err
	}
	otel.SetMeterProvider(mp)
	gs.RegisterStopper("otel-metrics", mp.Shutdown)
	// Feed Go runtime metrics (GC, heap, goroutines, GOMAXPROCS, ...) into
	// the MeterProvider we just built. The instrumentation registers async
	// callbacks on this provider; they are torn down by mp.Shutdown above,
	// so there is no separate stop hook to manage. startRuntime is guarded so
	// a re-run of setup (e.g. across gs.RunTest) does not register duplicate
	// callbacks on an already-instrumented provider.
	if cfg.Runtime.Enable {
		opts := []runtimemetrics.Option{runtimemetrics.WithMeterProvider(mp)}
		if cfg.Runtime.MinReadMemStatsInterval > 0 {
			opts = append(opts, runtimemetrics.WithMinimumReadMemStatsInterval(cfg.Runtime.MinReadMemStatsInterval))
		}
		if err := startRuntime(opts); err != nil {
			return err
		}
		log.Infof(context.Background(), starterTag, "runtime metrics enabled")
	}
	// Pull-based (prometheus) exporter: contribute the scrape handler as an
	// endpoint.Endpoint so starter-actuator, if present, serves /metrics on
	// the shared management port - no cross-starter import. The dedicated
	// server (ps.Server) is optional and only runs when metrics.port>0.
	if ps != nil {
		if ps.Server != nil {
			gs.RegisterStopper("otel-metrics-scrape-server", ps.Server.Shutdown)
		}
		if ps.Handler != nil {
			r.Provide(metric.NewEndpoint(cfg.Path, ps.Handler)).
				Export(gs.As[endpoint.Endpoint]())
		}
	}

	log.Infof(context.Background(), starterTag, "metrics provider initialized exporter=%s runtime_metrics=%v", cfg.Exporter, cfg.Runtime.Enable)
	return nil
}

// startRuntime starts the OTel Go-runtime metrics instrumentation exactly
// once per process. runtimemetrics.Start registers async callbacks on the
// MeterProvider and is not idempotent, so a second call (e.g. across gs.RunTest
// re-runs) would register duplicate instruments. The first call's outcome is
// sticky: a later re-run reuses it rather than silently masking the original
// error or re-registering callbacks.
func startRuntime(opts []runtimemetrics.Option) error {
	runtimeOnce.Do(func() {
		runtimeErr = runtimemetrics.Start(opts...)
	})
	return runtimeErr
}
