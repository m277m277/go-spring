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

package bytecache

import (
	"context"
	"errors"
	"time"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/spring/data/cache"
)

// NewByteCache wraps a *redis.Pool as a [cache.ByteCache] - the raw
// bytes-native primitives the "redigo" driver layers a typed [cache.Cache]
// façade over. The driver registered in the starter's root package selects the
// pool bean by beanID; call this directly to build a ByteCache for ad-hoc use.
func NewByteCache(pool *redis.Pool) cache.ByteCache {
	return &redigoCache{pool}
}

type redigoCache struct{ pool *redis.Pool }

// GetBytes returns the raw bytes under key. A redis.ErrNil reply (key absent)
// is reported as (nil, [cache.ErrMiss]) - a plain miss, not a backend error.
func (c *redigoCache) GetBytes(_ context.Context, key string) ([]byte, error) {
	conn := c.pool.Get()
	defer conn.Close()
	b, err := redis.Bytes(conn.Do("GET", key))
	if errors.Is(err, redis.ErrNil) {
		return nil, cache.ErrMiss
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// SetBytes stores the raw bytes under key for ttl. A non-positive ttl means no
// expiry. ttl is applied in whole seconds; a positive sub-second ttl is
// rounded up to 1s.
func (c *redigoCache) SetBytes(_ context.Context, key string, val []byte, ttl time.Duration) error {
	conn := c.pool.Get()
	defer conn.Close()
	if ttl > 0 {
		sec := int(ttl.Seconds())
		if sec < 1 {
			sec = 1
		}
		_, err := conn.Do("SET", key, val, "EX", sec)
		return err
	}
	_, err := conn.Do("SET", key, val)
	return err
}

// Delete removes key. Deleting an absent key is not an error.
func (c *redigoCache) Delete(_ context.Context, key string) error {
	conn := c.pool.Get()
	defer conn.Close()
	_, err := conn.Do("DEL", key)
	return err
}
