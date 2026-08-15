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

// Package cache defines a backend-pluggable abstraction for
// key/value caching, so a caching concern can be declared once and served by
// any backend.
//
// A backend implements the single [ByteCache] interface: the raw
// [ByteCache.GetBytes]/[ByteCache.SetBytes]/[ByteCache.Delete] primitives a
// remote client maps 1:1 to its native API. A [Cache] struct wraps a ByteCache
// and layers a pluggable [Codec] (default [JSONCodec]) on top, exposing typed
// [Cache.Get]/[Cache.Set] that cross the bytes/any boundary. A missing key is
// reported as [ErrMiss], distinct from a backend error, so callers fall through
// to the source of truth only on a real miss.
//
// This package is container-free: it imports no IoC concepts at all. The gs
// wiring that turns ${spring.cache} entries into Cache beans — the driver
// registry ("go-redis", "redigo", "bigcache", "memcached", ...) and the
// ${spring.cache} module — lives in the starter-cache module, next to nothing
// else; each backend starter registers its driver there.
// `spring.cache.main.driver=go-redis:main` exposes a Cache bean backed by the
// "main" redis client, exactly as before the split.
package cache

import (
	"context"
	"errors"
	"time"
)

// ByteCache is the bytes-native interface a caching backend implements: the
// raw primitives a remote client (Redis, memcached, bigcache) maps 1:1 to its
// native API. Implementations must be safe for concurrent use. A nil ByteCache
// is never valid; callers that want "no cache" should skip the lookup entirely
// rather than pass nil.
type ByteCache interface {
	// GetBytes returns the raw bytes stored under key. A missing key is
	// reported as (nil, [ErrMiss]).
	GetBytes(ctx context.Context, key string) ([]byte, error)

	// SetBytes stores the raw bytes under key for ttl. A non-positive ttl
	// means the entry does not expire.
	SetBytes(ctx context.Context, key string, val []byte, ttl time.Duration) error

	// Delete removes key. Deleting an absent key is not an error.
	Delete(ctx context.Context, key string) error
}

// Cache is the typed façade over a [ByteCache]. It embeds a ByteCache and adds
// [Cache.Get]/[Cache.Set], which cross the bytes/any boundary through a
// pluggable [Codec] (default [JSONCodec]); the raw [ByteCache.GetBytes]/
// [ByteCache.SetBytes]/[ByteCache.Delete] methods are promoted unchanged for
// callers that already hold bytes. A zero Cache is not usable - its ByteCache
// field must be set before any method is called. Since the embedded ByteCache
// must be safe for concurrent use, a Cache wrapping it is too.
type Cache struct {
	ByteCache
}

// Get decodes the value stored under key into val (which must be a pointer),
// using codec (default [JSONCodec]) to cross the bytes/any boundary. A missing
// key is reported as [ErrMiss]; any other error is a backend failure. On a
// miss the caller typically falls through to the source of truth.
func (c Cache) Get(ctx context.Context, key string, val any, codec ...Codec) error {
	b, err := c.GetBytes(ctx, key)
	if err != nil {
		return err
	}
	return ResolveCodec(codec).Unmarshal(b, val)
}

// Set encodes val with codec (default [JSONCodec]) and stores it under key for
// ttl. A non-positive ttl means the entry does not expire.
func (c Cache) Set(ctx context.Context, key string, val any, ttl time.Duration, codec ...Codec) error {
	b, err := ResolveCodec(codec).Marshal(val)
	if err != nil {
		return err
	}
	return c.SetBytes(ctx, key, b, ttl)
}

// ErrMiss is returned by Get/GetBytes when the key is absent (a cache miss).
// It is distinct from a backend error (network, serialization, ...): callers
// fall through to the source of truth only on a miss, not on a real failure.
var ErrMiss = errors.New("cache: miss")
