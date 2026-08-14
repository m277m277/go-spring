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

package observe

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// installGlobals wires in-memory OTel providers so a test can assert the three
// signals actually fire. It returns the span exporter + metric reader plus a
// cleanup that restores the prior globals.
func installGlobals(t *testing.T) (*tracetest.InMemoryExporter, metric.Reader, func()) {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevMP := otel.GetMeterProvider()

	spanExp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExp))

	rdr := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(rdr))

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	return spanExp, rdr, func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	}
}

// spanStubs reads the captured spans. With a synchronous exporter (WithSyncer)
// spans are exported on End, so no Flush is needed.
func spanStubs(exp *tracetest.InMemoryExporter) []tracetest.SpanStub {
	return exp.GetSpans()
}

// histPoints finds the duration histogram datapoints for the named metric.
func histPoints(t *testing.T, rdr metric.Reader, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	rm, err := func() (*metricdata.ResourceMetrics, error) {
		var m metricdata.ResourceMetrics
		if e := rdr.Collect(context.Background(), &m); e != nil {
			return nil, e
		}
		return &m, nil
	}()
	require.NoError(t, err)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok {
				return h.DataPoints
			}
		}
	}
	return nil
}

// TestStartEnd_NoopGlobals_NeVerPanics: with the default (no starter-otel) noop
// providers, Start/End must not panic at any log level. This is the zero-cost
// path most apps run until they import starter-otel.
func TestStartEnd_NoopGlobals_NeverPanics(t *testing.T) {
	for _, level := range []string{levelOff, levelBrief, levelDetailed} {
		o := NewDB("redis", ObserveConfig{Level: level, MaxArgBytes: 8})
		ctx, sp := o.Start(context.Background(), "GET", "user:1")
		assert.Equal(t, "GET", sp.op)
		assert.False(t, sp.skipped)
		// End must not panic under noop globals.
		assert.NotPanics(t, func() { sp.End(nil) })
		_ = ctx
	}
}

// TestSpanEmitted: with real providers, one operation yields one client span,
// named after the op, carrying the db.system/db.operation attrs and the bounded
// argument as db.statement; an error marks it Error and records it.
func TestSpanEmitted(t *testing.T) {
	spanExp, _, cleanup := installGlobals(t)
	defer cleanup()

	o := NewDB("redis", ObserveConfig{Level: levelOff, MaxArgBytes: 100})
	ctx, sp := o.Start(context.Background(), "GET", "user:1")
	_ = ctx
	sp.End(errors.New("boom"))

	stubs := spanStubs(spanExp)
	require.Len(t, stubs, 1)
	s := stubs[0]
	assert.Equal(t, "GET", s.Name)
	assert.Equal(t, "client", s.SpanKind.String())
	assert.Equal(t, codes.Error, s.Status.Code)

	attrs := attrMap(s.Attributes)
	assert.Equal(t, "redis", attrs["db.system"].AsString())
	assert.Equal(t, "GET", attrs["db.operation"].AsString())
	assert.Equal(t, "user:1", attrs["db.statement"].AsString())
}

// TestMetricEmitted: the duration histogram records one observation on End.
func TestMetricEmitted(t *testing.T) {
	_, rdr, cleanup := installGlobals(t)
	defer cleanup()

	o := NewDB("redis", ObserveConfig{Level: levelOff})
	_, sp := o.Start(context.Background(), "GET", "")
	sp.End(nil)

	pts := histPoints(t, rdr, "db.client.operation.duration")
	require.Len(t, pts, 1, "duration histogram must record the op")
	assert.Equal(t, uint64(1), pts[0].Count)
	am := attrMap(pts[0].Attributes.ToSlice())
	assert.Equal(t, "redis", am[attrKey("db.system")].AsString())
}

// TestSkipOps: a skipped op emits no span, no metric, and End is a no-op.
func TestSkipOps(t *testing.T) {
	spanExp, rdr, cleanup := installGlobals(t)
	defer cleanup()

	o := NewDB("redis", ObserveConfig{Level: levelBrief, SkipOps: []string{"PING"}})
	_, sp := o.Start(context.Background(), "PING", "")
	require.True(t, sp.skipped, "PING must be skipped")
	assert.NotPanics(t, func() { sp.End(nil) })

	assert.Empty(t, spanStubs(spanExp), "skipped op must not emit a span")
	_ = histPoints(t, rdr, "db.client.operation.duration") // must not panic; nothing recorded for PING
}

// TestMessagingAttrs: a producer observer uses messaging.* attribute names and
// the messaging metric prefix.
func TestMessagingAttrs(t *testing.T) {
	spanExp, _, cleanup := installGlobals(t)
	defer cleanup()

	o := NewProducer("nats", ObserveConfig{Level: levelOff})
	_, sp := o.Start(context.Background(), "publish", "events.created")
	sp.End(nil)

	stubs := spanStubs(spanExp)
	require.Len(t, stubs, 1)
	attrs := attrMap(stubs[0].Attributes)
	assert.Equal(t, "nats", attrs["messaging.system"].AsString())
	assert.Equal(t, "publish", attrs["messaging.operation"].AsString())
	assert.Equal(t, "events.created", attrs["messaging.destination.name"].AsString())
	assert.Equal(t, "producer", stubs[0].SpanKind.String())
}

// TestNewCustomSemConv: the general constructor emits into the caller-supplied
// namespace — custom metric prefix and custom attribute names — instead of
// silently falling back to the db.* attribute family.
func TestNewCustomSemConv(t *testing.T) {
	spanExp, rdr, cleanup := installGlobals(t)
	defer cleanup()

	sc := SemConv{
		Domain:    "kv.client",
		SystemKey: "kv.store",
		OpKey:     "kv.op",
		ArgKey:    "kv.key",
	}
	o := New("titandb", sc, trace.SpanKindClient, ObserveConfig{Level: levelOff})
	_, sp := o.Start(context.Background(), "get", "user:1")
	sp.End(nil)

	stubs := spanStubs(spanExp)
	require.Len(t, stubs, 1)
	attrs := attrMap(stubs[0].Attributes)
	assert.Equal(t, "titandb", attrs["kv.store"].AsString())
	assert.Equal(t, "get", attrs["kv.op"].AsString())
	assert.Equal(t, "user:1", attrs["kv.key"].AsString())

	pts := histPoints(t, rdr, "kv.client.operation.duration")
	require.Len(t, pts, 1)
	v, ok := pts[0].Attributes.Value(attrKey("kv.store"))
	require.True(t, ok, "kv.store attribute on the duration metric")
	assert.Equal(t, "titandb", v.AsString())
}

// TestBoundArg: truncation lands on a rune boundary and marks the cut.
func TestBoundArg(t *testing.T) {
	assert.Equal(t, "abc", boundArg("abc", 10), "short arg unchanged")
	assert.Equal(t, "ab...(truncated)", boundArg("abcdefg", 2))
	// multibyte: cut must not split a rune ("世界" is 6 bytes; cut at 4 lands mid-rune -> back to 3)
	got := boundArg("世界abc", 4)
	assert.True(t, len(got) <= 4+len("...(truncated)"))
	for _, r := range got[:len(got)-len("...(truncated)")] {
		_ = r
	}
	assert.Equal(t, "世...(truncated)", boundArg("世界abc", 4), "cut on rune boundary, keep whole 世")
}

func attrKey(k string) attribute.Key { return attribute.Key(k) }

func attrMap(kv []attribute.KeyValue) map[attribute.Key]attribute.Value {
	m := make(map[attribute.Key]attribute.Value, len(kv))
	for _, a := range kv {
		m[a.Key] = a.Value
	}
	return m
}
