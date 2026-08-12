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

package aspect

import (
	"sync"
	"time"
)

// KeyFunc derives a cache key from a joinpoint. Returning an empty string tells
// [Cache] to skip caching for this invocation (neither read nor write).
type KeyFunc func(jp *Joinpoint) string

// Store is the backend a [Cache] interceptor reads and writes. Implementations
// must be safe for concurrent use. The bundled [MemoryStore] is a zero-dependency
// in-process implementation; a starter may adapt Redis or another cache to this
// interface without changing the interceptor.
type Store interface {
	// Get returns the cached value for key and whether it was present (and not
	// expired).
	Get(key string) (any, bool)
	// Set stores val under key for the given ttl. A non-positive ttl means the
	// entry does not expire.
	Set(key string, val any, ttl time.Duration)
}

// Cache returns an interceptor that provides the @Cacheable equivalent: on a hit
// it returns the stored value and skips the rest of the chain entirely; on a miss
// it proceeds and, when the operation succeeds, stores the result under ttl. When
// key returns an empty string caching is bypassed for that invocation. A nil
// store makes the interceptor a transparent pass-through.
func Cache(store Store, key KeyFunc, ttl time.Duration) Interceptor {
	return InterceptorFunc(func(jp *Joinpoint) (any, error) {
		if store == nil || key == nil {
			return jp.Proceed(jp.Context)
		}
		k := key(jp)
		if k == "" {
			return jp.Proceed(jp.Context)
		}
		if v, ok := store.Get(k); ok {
			jp.Result = v
			return v, nil
		}
		v, err := jp.Proceed(jp.Context)
		if err == nil {
			store.Set(k, v, ttl)
		}
		return v, err
	})
}

// MemoryStore is a zero-dependency, concurrency-safe in-process [Store] with
// per-entry expiry. It is intended for tests and single-instance caching; use a
// shared cache (Redis, memcached) behind the [Store] interface for multi-replica
// deployments. The zero value is ready to use.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
}

type memoryEntry struct {
	val      any
	expireAt time.Time // zero means no expiry
}

// Get implements [Store]. An expired entry is treated as absent and lazily
// dropped.
func (m *MemoryStore) Get(key string) (any, bool) {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !e.expireAt.IsZero() && time.Now().After(e.expireAt) {
		m.mu.Lock()
		// Re-check under the write lock: a concurrent Set may have refreshed it.
		if cur, ok := m.entries[key]; ok && cur.expireAt.Equal(e.expireAt) {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		return nil, false
	}
	return e.val, true
}

// Set implements [Store]. A non-positive ttl stores the entry without expiry.
func (m *MemoryStore) Set(key string, val any, ttl time.Duration) {
	e := memoryEntry{val: val}
	if ttl > 0 {
		e.expireAt = time.Now().Add(ttl)
	}
	m.mu.Lock()
	if m.entries == nil {
		m.entries = make(map[string]memoryEntry)
	}
	m.entries[key] = e
	m.mu.Unlock()
}
