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

package traffic

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

// Carrier is the generic string-multi-map shape both [net/http.Header] and a
// gRPC metadata.MD satisfy (a metadata.MD is literally a map[string][]string).
// The propagator reads and writes the marker through a Carrier so the
// protocol-specific glue stays out of this stdlib-only package: starter-grpc
// casts its metadata.MD to Carrier and reuses the same helpers starter-gin uses
// on an http.Header.
type Carrier map[string][]string

// Has reports whether c carries the load-test marker under key. Key matching is
// canonicalised by the caller: HTTP callers pass [HeaderLoadTest] and rely on
// [net/http.Header.Get] case folding; gRPC callers pass [MetaKeyLoadTest]
// (metadata keys are already lowercased by gRPC).
func (c Carrier) Has(key string) bool {
	if c == nil {
		return false
	}
	return slices.ContainsFunc(c[key], isTruthy)
}

// Set writes the load-test marker into c under key. It is idempotent: calling
// it twice does not append a second value.
func (c Carrier) Set(key string) {
	if c == nil {
		return
	}
	c[key] = []string{"1"}
}

// Propagator ferries the load-test marker across a process boundary. It carries
// the carrier-key names so an installation that has standardised on a different
// header (e.g. "X-Stress", "X-Shadow") can customise once at startup rather
// than rewire every starter. The zero value is NOT usable; always construct via
// [NewPropagator].
type Propagator struct {
	// HTTPHeader is the key read/written on an [net/http.Header]. Defaults to
	// [HeaderLoadTest].
	HTTPHeader string
	// MetaKey is the key read/written on a gRPC metadata.MD (a [Carrier]).
	// Defaults to [MetaKeyLoadTest].
	MetaKey string
}

// NewPropagator returns a Propagator using the package defaults
// ([HeaderLoadTest], [MetaKeyLoadTest]).
func NewPropagator() *Propagator {
	return &Propagator{
		HTTPHeader: HeaderLoadTest,
		MetaKey:    MetaKeyLoadTest,
	}
}

// ExtractHTTP returns ctx tagged as load-test traffic when req carries the
// marker header; otherwise it returns ctx unchanged. The marker's source is
// recorded as "http-header" so [Source] can report the entry point in logs.
//
// It is the inbound seam for HTTP servers: a gin/echo/hertz middleware calls
// ExtractHTTP on the incoming request and threads the returned ctx through the
// handler chain.
//
// The header lookup goes through [net/http.Header.Get], which canonicalises the
// key, so any case spelling of the header name matches. The raw [Carrier] map
// helpers are NOT used here because HTTP header keys are textproto-canonicalised
// on insert (e.g. "X-LoadTest" is stored as "X-Loadtest"), which a plain map
// lookup would miss.
func (p *Propagator) ExtractHTTP(ctx context.Context, req *http.Request) context.Context {
	if p == nil || req == nil {
		return ctx
	}
	if !isTruthy(req.Header.Get(p.HTTPHeader)) {
		return ctx
	}
	return WithLoadTest(ctx, "http-header")
}

// InjectHTTP writes the marker header onto req when ctx is a load-test context;
// otherwise it is a no-op. It is the outbound seam for HTTP clients: before
// sending a request, an http.RoundTripper (or the starter's transport) calls
// InjectHTTP so the downstream hop can recognise the traffic. The header is set
// via [net/http.Header.Set] so the key is canonicalised correctly.
func (p *Propagator) InjectHTTP(ctx context.Context, req *http.Request) {
	if p == nil || req == nil || !IsLoadTest(ctx) {
		return
	}
	req.Header.Set(p.HTTPHeader, "1")
}

// IsAffirmative reports whether s is an affirmative load-test marker value
// ("1", "true", "on", "yes", "t", case-insensitive). Server starters (gin,
// echo, hertz, ...) use it to test a raw inbound header/metadata value before
// tagging the request context via [WithLoadTest], so the truthy rule stays in
// one place. Any other value (including "0", "false", "no", "") is negative.
func IsAffirmative(s string) bool { return isTruthy(s) }

// isTruthy reports whether s carries a load-test-affirmative value. The marker
// header is considered set by any of "1", "true", "on", "yes", "t"
// (case-insensitive); any other value (including "0", "false", "no", "") is
// treated as absent.
func isTruthy(s string) bool {
	for _, v := range []string{"1", "true", "on", "yes", "t"} {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// ExtractCarrier is the inbound seam for non-HTTP protocols whose metadata is a
// [Carrier] (notably gRPC metadata.MD). It tags ctx when c carries the marker
// under p.MetaKey; the source is recorded as source (e.g. "grpc-metadata").
func (p *Propagator) ExtractCarrier(ctx context.Context, c Carrier, source string) context.Context {
	if p == nil || !c.Has(p.MetaKey) {
		return ctx
	}
	return WithLoadTest(ctx, source)
}

// InjectCarrier is the outbound seam for non-HTTP protocols whose metadata is a
// [Carrier]. When ctx is a load-test context it writes the marker under
// p.MetaKey; otherwise it is a no-op.
func (p *Propagator) InjectCarrier(ctx context.Context, c Carrier) {
	if p == nil || !IsLoadTest(ctx) {
		return
	}
	c.Set(p.MetaKey)
}

// defaultPropagator is the shared package-default Propagator; the package-level
// helpers below delegate to it so convenience callers do not allocate per call.
// It is never mutated, so concurrent use is safe.
var defaultPropagator = NewPropagator()

// ExtractHTTP tags ctx from the inbound request's default header
// ([HeaderLoadTest]). It delegates to the package-default [Propagator].
func ExtractHTTP(ctx context.Context, req *http.Request) context.Context {
	return defaultPropagator.ExtractHTTP(ctx, req)
}

// InjectHTTP writes the default marker header ([HeaderLoadTest]) onto req when
// ctx is a load-test context. It delegates to the package-default [Propagator].
func InjectHTTP(ctx context.Context, req *http.Request) {
	defaultPropagator.InjectHTTP(ctx, req)
}
