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

package StarterHTTPClient

import (
	"net/http"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// stubRT is a no-op RoundTripper for registry tests.
type stubRT struct{ tag string }

func (s *stubRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
}

func resetExtensions(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		extMu.Lock()
		baseFactory = nil
		transportWrappers = nil
		extMu.Unlock()
	})
}

func TestBaseFactoryLastWinsAndNilReverts(t *testing.T) {
	resetExtensions(t)
	assert.That(t, currentBaseFactory() == nil).True()

	SetBaseTransportFactory(func(name string, _ Config) http.RoundTripper { return &stubRT{tag: "a"} })
	assert.That(t, currentBaseFactory() != nil).True()

	SetBaseTransportFactory(func(name string, _ Config) http.RoundTripper { return &stubRT{tag: "b"} })
	assert.That(t, currentBaseFactory()("x", Config{}).(*stubRT).tag).Equal("b")

	SetBaseTransportFactory(nil)
	assert.That(t, currentBaseFactory() == nil).True()
}

func TestTransportMiddlewareOrderingAndSnapshot(t *testing.T) {
	resetExtensions(t)
	assert.That(t, len(currentTransportWrappers())).Equal(0)

	var order []string
	// Each middleware records its name when the RETURNED RoundTripper handles a
	// request (request-time), then delegates — so `order` reflects the actual
	// call order on the wire, not construction order.
	mk := func(tag string) TransportMiddleware {
		return func(_ string, _ Config, next http.RoundTripper) http.RoundTripper {
			return roundTripFunc(func(req *http.Request) (*http.Response, error) {
				order = append(order, tag)
				return next.RoundTrip(req)
			})
		}
	}
	UseTransportMiddleware(mk("first"))
	UseTransportMiddleware(mk("second"))

	snap := currentTransportWrappers()
	assert.That(t, len(snap)).Equal(2)

	// Compose as newClient does: iterate in reverse so first-registered is
	// outermost (runs first on the request path).
	var rt http.RoundTripper = &stubRT{tag: "base"}
	for _, w := range reverseFor(snap) {
		rt = w("x", Config{}, rt)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	_, _ = rt.RoundTrip(req)
	// "first" ran before "second" because first-registered is outermost.
	assert.That(t, order).Equal([]string{"first", "second"})

	// Snapshot is a copy: registering more must not change the captured slice.
	UseTransportMiddleware(func(_ string, _ Config, next http.RoundTripper) http.RoundTripper { return next })
	assert.That(t, len(snap)).Equal(2)
}

// roundTripFunc adapts a function into an http.RoundTripper for tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// reverseFor returns a new slice in reverse order, mirroring the slices.Backward
// loop newClient uses, without importing slices in the test.
func reverseFor(in []TransportMiddleware) []TransportMiddleware {
	out := make([]TransportMiddleware, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
