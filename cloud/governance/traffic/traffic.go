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

// Package traffic is the cross-process load-test traffic identification layer
// for go-spring. It answers one question — "is the request in flight a
// load-test request?" — and propagates the answer across process boundaries so
// every hop in a call chain can tell synthetic load apart from real traffic.
//
// Why this exists. The companion package [go-spring.org/cloud/loadtest] only
// generates load; the requests it produces are indistinguishable from real
// ones once they leave the process. Without an identifier there is no way to
// route load-test traffic to a shadow database, to isolate its circuit-breaker
// pool, to skip fault injection for it (or apply it only to it), or simply to
// tag the resulting metrics. traffic fills that gap with the smallest possible
// primitive: a marker carried in the [context.Context] and a propagator that
// ferries it across HTTP headers and gRPC metadata.
//
// Layering. traffic is the most foundational cross-cutting concern in the
// cloud stack — everything else (observe, resilience, fault) may consume it but
// it consumes nothing from them. It stays stdlib-only and dependency-free so
// any starter can import it without pulling a heavier graph. The HTTP
// propagation uses only [net/http]; the gRPC-specific wiring (which needs
// google.golang.org/grpc/metadata) lives in starter-grpc and adapts the generic
// [Carrier] helpers here, since a gRPC metadata.MD is itself a
// map[string][]string.
//
// Consumption is opt-in. Marking a context and propagating the marker costs
// almost nothing; deciding what to DO with a load-test request (shadow table,
// isolated breaker, metric tag) is intentionally NOT built in. Those are
// per-component layers the user adds — see the design doc for the onion-model
// rationale. traffic only carries the flag; it never acts on it.
package traffic

import (
	"context"
)

// HeaderLoadTest is the canonical HTTP header carrying the load-test marker.
// It is also read case-insensitively by the [Propagator] via
// [net/http.Header.Get], so "x-loadtest" / "X-LoadTest" / "X-LOADTEST" all match.
const HeaderLoadTest = "X-LoadTest"

// MetaKeyLoadTest is the gRPC metadata key carrying the load-test marker.
// gRPC requires lowercase metadata keys, so the HTTP header name is not reused
// verbatim. starter-grpc adapts a metadata.MD to the generic [Carrier] and uses
// this key.
const MetaKeyLoadTest = "x-loadtest"

// marker is the value type stored under ctxKey. It is unexported so the only
// way to set it is via [WithLoadTest]; callers test for presence with
// [IsLoadTest]. Carrying a Source string (rather than a bare bool) lets
// observability report where the flag entered the process — "http-header",
// "grpc-metadata", "loadtest.Run" — without expanding the public API when new
// entry points are added. A present-but-zero marker still counts as a load-test
// request; Source is purely informational.
type marker struct {
	source string
}

type ctxKey struct{}

// WithLoadTest returns a copy of ctx tagged as carrying load-test traffic. The
// optional source records where the marker originated (a free-form label for
// telemetry, e.g. "http-header" or "loadtest.Run"); only the first value is
// kept when several are supplied. Tagging an already-tagged context keeps the
// existing source so the original entry point is preserved across hops.
//
// Passing load-test context to real downstream systems is safe: the marker is
// inert until a consumer reads it. To act on it, see [IsLoadTest].
func WithLoadTest(ctx context.Context, source ...string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(ctxKey{}).(marker); ok {
		return ctx // keep the original source across propagation
	}
	src := ""
	if len(source) > 0 {
		src = source[0]
	}
	return context.WithValue(ctx, ctxKey{}, marker{source: src})
}

// IsLoadTest reports whether ctx carries the load-test marker set by
// [WithLoadTest] or extracted from an inbound carrier by [Propagator.Extract].
// It is the single read-side primitive every consumer (shadow router,
// breaker-isolation layer, metric tagger, fault gate) uses.
func IsLoadTest(ctx context.Context) bool {
	_, ok := ctx.Value(ctxKey{}).(marker)
	return ok
}

// Source reports the free-form label recorded when the marker was set, or the
// empty string if ctx is not a load-test context (or was tagged without a
// source). Useful for access logs and metrics dimensions.
func Source(ctx context.Context) string {
	m, ok := ctx.Value(ctxKey{}).(marker)
	if !ok {
		return ""
	}
	return m.source
}

// Propagate copies the load-test marker from parent onto child if parent is a
// load-test context, preserving the original source. It is the cross-goroutine
// / cross-API continuation helper: when a handler fans work out to a goroutine
// or a new background context, call Propagate(parent, child) so the child
// inherits the flag. If parent is not a load-test context, child is returned
// unchanged.
func Propagate(parent, child context.Context) context.Context {
	if parent == nil {
		return child
	}
	m, ok := parent.Value(ctxKey{}).(marker)
	if !ok {
		return child
	}
	if child == nil {
		child = context.Background()
	}
	return context.WithValue(child, ctxKey{}, m)
}
