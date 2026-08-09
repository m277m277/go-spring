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

package cache

import (
	"context"
	"errors"
	"time"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/spring/data/cache"
)

// NewCache wraps a *redis.Pool as a [cache.ByteCache], embedded in a
// [cache.Cache] façade that supplies the typed Get/Set codec layer. The
// "redigo" driver registered in the starter's root package wires it over the
// pool bean selected by beanID; use it directly for programmatic construction
// too.
func NewCache(pool *redis.Pool) *cache.Cache {
	return &cache.Cache{ByteCache: &redigoCache{pool}}
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
