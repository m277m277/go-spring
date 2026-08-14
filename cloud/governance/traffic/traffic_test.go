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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkerRoundTrip(t *testing.T) {
	ctx := context.Background()
	assert.False(t, IsLoadTest(ctx))
	assert.Equal(t, "", Source(ctx))

	tagged := WithLoadTest(ctx, "loadtest.Run")
	assert.True(t, IsLoadTest(tagged))
	assert.Equal(t, "loadtest.Run", Source(tagged))

	// tagging again keeps the original source
	again := WithLoadTest(tagged, "other")
	assert.Equal(t, "loadtest.Run", Source(again))
}

func TestMarkerWithoutSource(t *testing.T) {
	ctx := WithLoadTest(context.Background())
	assert.True(t, IsLoadTest(ctx))
	assert.Equal(t, "", Source(ctx))
}

func TestWithLoadTestNilContext(t *testing.T) {
	var nilCtx context.Context
	ctx := WithLoadTest(nilCtx, "x")
	require.NotNil(t, ctx)
	assert.True(t, IsLoadTest(ctx))
}

func TestPropagate(t *testing.T) {
	parent := WithLoadTest(context.Background(), "http-header")

	// child inherits marker + source
	child := Propagate(parent, context.Background())
	assert.True(t, IsLoadTest(child))
	assert.Equal(t, "http-header", Source(child))

	// nil parent => child unchanged (still not a load-test ctx)
	var nilCtx context.Context
	plain := context.Background()
	out2 := Propagate(nilCtx, plain)
	assert.False(t, IsLoadTest(out2))

	// non-load-test parent => child loses the marker it never had
	assert.False(t, IsLoadTest(Propagate(context.Background(), context.Background())))
}

func TestPropagateNilChild(t *testing.T) {
	parent := WithLoadTest(context.Background(), "grpc-metadata")
	out := Propagate(parent, nil)
	require.NotNil(t, out)
	assert.True(t, IsLoadTest(out))
}

func TestCarrierHasAcceptsTruthyValues(t *testing.T) {
	c := Carrier{}
	assert.False(t, c.Has("k"))

	for _, v := range []string{"1", "true", "on", "TRUE", "On"} {
		c["k"] = []string{v}
		assert.True(t, c.Has("k"), "value %q should be truthy", v)
	}
	for _, v := range []string{"0", "false", "", "no"} {
		c["k"] = []string{v}
		assert.False(t, c.Has("k"), "value %q should not be truthy", v)
	}
}

func TestCarrierSetIsIdempotent(t *testing.T) {
	c := Carrier{}
	c.Set("k")
	c.Set("k")
	assert.Len(t, c["k"], 1)
}

func TestCarrierNilSafe(t *testing.T) {
	var c Carrier
	assert.NotPanics(t, func() {
		c.Set("k")
		_ = c.Has("k")
	})
	assert.False(t, c.Has("k"))
}

func TestPropagatorExtractInjectHTTP(t *testing.T) {
	p := NewPropagator()
	require.Equal(t, HeaderLoadTest, p.HTTPHeader)
	require.Equal(t, MetaKeyLoadTest, p.MetaKey)

	// inbound: header present => ctx tagged
	req := &http.Request{Header: http.Header{}}
	req.Header.Set(HeaderLoadTest, "1")
	ctx := p.ExtractHTTP(context.Background(), req)
	assert.True(t, IsLoadTest(ctx))
	assert.Equal(t, "http-header", Source(ctx))

	// inbound: header absent => ctx unchanged
	plain := &http.Request{Header: http.Header{}}
	out := p.ExtractHTTP(context.Background(), plain)
	assert.False(t, IsLoadTest(out))

	// outbound: tagged ctx => header written
	outReq := &http.Request{Header: http.Header{}}
	p.InjectHTTP(ctx, outReq)
	assert.Equal(t, "1", outReq.Header.Get(HeaderLoadTest))

	// outbound: untagged ctx => no header
	outPlain := &http.Request{Header: http.Header{}}
	p.InjectHTTP(context.Background(), outPlain)
	assert.Empty(t, outPlain.Header.Get(HeaderLoadTest))
}

func TestPropagatorExtractInjectCarrier(t *testing.T) {
	p := NewPropagator()

	// inbound: metadata carrier present => ctx tagged
	md := Carrier{MetaKeyLoadTest: []string{"1"}}
	ctx := p.ExtractCarrier(context.Background(), md, "grpc-metadata")
	assert.True(t, IsLoadTest(ctx))
	assert.Equal(t, "grpc-metadata", Source(ctx))

	// inbound: carrier without marker => unchanged
	assert.False(t, IsLoadTest(p.ExtractCarrier(context.Background(), Carrier{}, "grpc-metadata")))

	// outbound: tagged ctx => carrier written
	out := Carrier{}
	p.InjectCarrier(ctx, out)
	assert.True(t, out.Has(MetaKeyLoadTest))

	// outbound: untagged ctx => carrier untouched
	out2 := Carrier{}
	p.InjectCarrier(context.Background(), out2)
	assert.False(t, out2.Has(MetaKeyLoadTest))
}

func TestPropagatorCustomHeader(t *testing.T) {
	p := &Propagator{HTTPHeader: "X-Stress", MetaKey: "x-stress"}

	req := &http.Request{Header: http.Header{}}
	req.Header.Set("X-Stress", "1")
	ctx := p.ExtractHTTP(context.Background(), req)
	assert.True(t, IsLoadTest(ctx))

	outReq := &http.Request{Header: http.Header{}}
	p.InjectHTTP(ctx, outReq)
	assert.Equal(t, "1", outReq.Header.Get("X-Stress"))
	// default header must NOT be set when customised
	assert.Empty(t, outReq.Header.Get(HeaderLoadTest))
}

func TestPackageLevelHelpers(t *testing.T) {
	req := &http.Request{Header: http.Header{}}
	req.Header.Set(HeaderLoadTest, "1")
	ctx := ExtractHTTP(context.Background(), req)
	assert.True(t, IsLoadTest(ctx))

	outReq := &http.Request{Header: http.Header{}}
	InjectHTTP(ctx, outReq)
	assert.Equal(t, "1", outReq.Header.Get(HeaderLoadTest))
}

func TestExtractHTTPHeaderCaseInsensitive(t *testing.T) {
	// http.Header.Get canonicalises, so any case spelling matches.
	for _, spelling := range []string{"x-loadtest", "X-LOADTEST", "X-LoadTest"} {
		req := &http.Request{Header: http.Header{}}
		req.Header.Set(spelling, "1")
		ctx := ExtractHTTP(context.Background(), req)
		assert.True(t, IsLoadTest(ctx), "spelling %q should match", spelling)
	}
}
