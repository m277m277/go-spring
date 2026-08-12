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

package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go-spring.org/spring/data/cache"
	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/testing/assert"
)

// noopDriver is a Driver that builds no beans, used to exercise the registry
// without pulling in a real backend.
var noopDriver cache.Driver = func(string) gs.ModuleFunc { return nil }

func TestJSONCodecRoundTrip(t *testing.T) {
	var c cache.JSONCodec
	in := map[string]any{"a": float64(1), "b": "x"}

	b, err := c.Marshal(in)
	assert.That(t, err).Nil()

	var out map[string]any
	assert.That(t, c.Unmarshal(b, &out)).Nil()
	assert.That(t, out).Equal(in)
}

func TestResolveCodec(t *testing.T) {
	// No override, or an explicit nil, both fall back to the default JSONCodec.
	assert.That(t, cache.ResolveCodec(nil)).Equal(cache.JSONCodec{})
	assert.That(t, cache.ResolveCodec([]cache.Codec{nil})).Equal(cache.JSONCodec{})

	// A provided codec wins.
	custom := &fakeCodec{}
	assert.That(t, cache.ResolveCodec([]cache.Codec{custom})).Equal(custom)
}

func TestErrMiss(t *testing.T) {
	// ErrMiss is a sentinel a caller distinguishes from a real backend error.
	assert.That(t, errors.Is(cache.ErrMiss, cache.ErrMiss)).True()
	assert.That(t, errors.Is(errors.New("boom"), cache.ErrMiss)).False()
}

func TestRegisterDriverPanics(t *testing.T) {
	assert.Panic(t, func() { cache.RegisterDriver("", noopDriver) }, "empty name")
	assert.Panic(t, func() { cache.RegisterDriver("test-nil-driver", nil) }, "nil driver")
}

func TestRegisterAndGetDriver(t *testing.T) {
	cache.RegisterDriver("test-noop-driver", noopDriver)

	_, err := cache.GetDriver("test-noop-driver")
	assert.That(t, err).Nil()

	// Re-registering the same name is a wiring bug; fail loudly at init.
	assert.Panic(t, func() {
		cache.RegisterDriver("test-noop-driver", noopDriver)
	}, "already registered")
}

func TestGetDriverNotFound(t *testing.T) {
	_, err := cache.GetDriver("test-does-not-exist")
	assert.That(t, err).NotNil()
	// The error lists what IS registered so a typo or a missing starter import
	// is obvious.
	assert.That(t, strings.Contains(err.Error(), "no driver registered")).True()
}

// fakeCodec is a no-op Codec used only to prove ResolveCodec returns the
// caller's codec by identity rather than the default.
type fakeCodec struct{}

func (fakeCodec) Marshal(v any) ([]byte, error)   { return nil, nil }
func (fakeCodec) Unmarshal(b []byte, v any) error { return nil }

// fakeByteCache is an in-memory ByteCache for exercising the Cache façade
// without a real backend. It stores raw bytes and reports ErrMiss on absent
// keys, so the codec layer above it can be tested in isolation.
type fakeByteCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newFakeByteCache() *fakeByteCache {
	return &fakeByteCache{m: make(map[string][]byte)}
}

func (f *fakeByteCache) GetBytes(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.m[key]
	if !ok {
		return nil, cache.ErrMiss
	}
	return b, nil
}

func (f *fakeByteCache) SetBytes(_ context.Context, key string, val []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = val
	return nil
}

func (f *fakeByteCache) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, key)
	return nil
}

// markerCodec wraps JSON with a "MARKER:" prefix so a test can prove the Cache
// façade routes Get/Set through the caller's Codec rather than the default.
type markerCodec struct{}

func (markerCodec) Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append([]byte("MARKER:"), b...), nil
}

func (markerCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(bytes.TrimPrefix(data, []byte("MARKER:")), v)
}

func TestCacheRoundTrip(t *testing.T) {
	bc := newFakeByteCache()
	c := cache.Cache{ByteCache: bc}

	type user struct{ Name string }
	in := user{Name: "ada"}

	// Set routes through the default JSON codec into the underlying ByteCache.
	assert.That(t, c.Set(context.Background(), "u:1", in, 0)).Nil()
	// The stored bytes are exactly what JSONCodec.Marshal produced - proving
	// the façade, not the caller, did the encode.
	want, _ := json.Marshal(in)
	assert.That(t, bc.m["u:1"]).Equal(want)

	var out user
	assert.That(t, c.Get(context.Background(), "u:1", &out)).Nil()
	assert.That(t, out).Equal(in)
}

func TestCacheGetMiss(t *testing.T) {
	c := cache.Cache{ByteCache: newFakeByteCache()}
	var v any
	err := c.Get(context.Background(), "absent", &v)
	assert.That(t, errors.Is(err, cache.ErrMiss)).True()
}

func TestCacheDelete(t *testing.T) {
	bc := newFakeByteCache()
	c := cache.Cache{ByteCache: bc}

	assert.That(t, c.Set(context.Background(), "k", 42, 0)).Nil()
	assert.That(t, c.Delete(context.Background(), "k")).Nil()

	var v int
	assert.That(t, errors.Is(c.Get(context.Background(), "k", &v), cache.ErrMiss)).True()
}

func TestCacheCustomCodec(t *testing.T) {
	bc := newFakeByteCache()
	c := cache.Cache{ByteCache: bc}

	in := map[string]any{"n": float64(7)}
	assert.That(t, c.Set(context.Background(), "k", in, 0, markerCodec{})).Nil()
	// The caller's codec ran, not the default JSON.
	assert.That(t, bytes.HasPrefix(bc.m["k"], []byte("MARKER:"))).True()

	var out map[string]any
	assert.That(t, c.Get(context.Background(), "k", &out, markerCodec{})).Nil()
	assert.That(t, out["n"]).Equal(float64(7))
}
