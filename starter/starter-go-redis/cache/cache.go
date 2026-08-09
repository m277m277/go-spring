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

	"github.com/redis/go-redis/v9"
	"go-spring.org/spring/data/cache"
)

// NewCache wraps a *redis.Client as a [cache.ByteCache], embedded in a
// [cache.Cache] façade that supplies the typed Get/Set codec layer. The
// "go-redis" driver registered in the starter's root package wires it over the
// client bean selected by beanID; use it directly for programmatic construction
// too.
func NewCache(c *redis.Client) *cache.Cache {
	return &cache.Cache{ByteCache: &redisCache{c}}
}

type redisCache struct{ c *redis.Client }

// GetBytes returns the raw bytes under key. A redis.Nil reply (key absent) is
// reported as (nil, [cache.ErrMiss]) - a plain miss, not a backend error - so
// callers fall through to the source of truth.
func (c *redisCache) GetBytes(ctx context.Context, key string) ([]byte, error) {
	b, err := c.c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, cache.ErrMiss
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// SetBytes stores the raw bytes under key for ttl. A non-positive ttl means no
// expiry (go-redis treats 0 as no TTL).
func (c *redisCache) SetBytes(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return c.c.Set(ctx, key, val, ttl).Err()
}

// Delete removes key. Deleting an absent key is not an error.
func (c *redisCache) Delete(ctx context.Context, key string) error {
	return c.c.Del(ctx, key).Err()
}
