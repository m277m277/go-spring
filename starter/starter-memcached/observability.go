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

package StarterMemcached

import (
	"context"
	"fmt"

	"github.com/bradfitz/gomemcache/memcache"
	observe "go-spring.org/observe"
)

// obsClient wraps *memcache.Client so every operation flows through the shared
// observe kit (trace span + duration/in-flight metric + access log). It embeds
// the real client, so methods not overridden below (only Close, a lifecycle
// method) are promoted unchanged. memcache's API carries no context, so spans
// are root spans (not linked to the caller's request trace) — a limitation of
// gomemcache, documented here.
type obsClient struct {
	*memcache.Client
	obs *observe.Observer
}

func newObsClient(c *memcache.Client, cfg observe.LogConfig) *obsClient {
	return &obsClient{Client: c, obs: observe.NewClient("memcached", cfg)}
}

// instrument wraps a single-key operation: op names it, key is the log/span arg.
// It returns an end callback the caller invokes with the operation's error.
func (c *obsClient) instrument(op, key string) func(error) {
	_, sp := c.obs.Start(context.Background(), op, key)
	return func(err error) { sp.End(err) }
}

func (c *obsClient) Get(key string) (*memcache.Item, error) {
	end := c.instrument("get", key)
	it, err := c.Client.Get(key)
	end(err)
	return it, err
}

func (c *obsClient) GetAndTouch(key string, expiration int32) (*memcache.Item, error) {
	end := c.instrument("get_and_touch", key)
	it, err := c.Client.GetAndTouch(key, expiration)
	end(err)
	return it, err
}

func (c *obsClient) GetMulti(keys []string) (map[string]*memcache.Item, error) {
	end := c.instrument("get_multi", fmt.Sprintf("%d keys", len(keys)))
	m, err := c.Client.GetMulti(keys)
	end(err)
	return m, err
}

func (c *obsClient) Touch(key string, seconds int32) error {
	end := c.instrument("touch", key)
	err := c.Client.Touch(key, seconds)
	end(err)
	return err
}

func (c *obsClient) Set(item *memcache.Item) error {
	end := c.instrument("set", item.Key)
	err := c.Client.Set(item)
	end(err)
	return err
}

func (c *obsClient) Add(item *memcache.Item) error {
	end := c.instrument("add", item.Key)
	err := c.Client.Add(item)
	end(err)
	return err
}

func (c *obsClient) Replace(item *memcache.Item) error {
	end := c.instrument("replace", item.Key)
	err := c.Client.Replace(item)
	end(err)
	return err
}

func (c *obsClient) Append(item *memcache.Item) error {
	end := c.instrument("append", item.Key)
	err := c.Client.Append(item)
	end(err)
	return err
}

func (c *obsClient) Prepend(item *memcache.Item) error {
	end := c.instrument("prepend", item.Key)
	err := c.Client.Prepend(item)
	end(err)
	return err
}

func (c *obsClient) CompareAndSwap(item *memcache.Item) error {
	end := c.instrument("cas", item.Key)
	err := c.Client.CompareAndSwap(item)
	end(err)
	return err
}

func (c *obsClient) Delete(key string) error {
	end := c.instrument("delete", key)
	err := c.Client.Delete(key)
	end(err)
	return err
}

func (c *obsClient) DeleteAll() error {
	end := c.instrument("delete_all", "")
	err := c.Client.DeleteAll()
	end(err)
	return err
}

func (c *obsClient) Increment(key string, delta uint64) (uint64, error) {
	end := c.instrument("increment", key)
	n, err := c.Client.Increment(key, delta)
	end(err)
	return n, err
}

func (c *obsClient) Decrement(key string, delta uint64) (uint64, error) {
	end := c.instrument("decrement", key)
	n, err := c.Client.Decrement(key, delta)
	end(err)
	return n, err
}

func (c *obsClient) Ping() error {
	end := c.instrument("ping", "")
	err := c.Client.Ping()
	end(err)
	return err
}

func (c *obsClient) FlushAll() error {
	end := c.instrument("flush_all", "")
	err := c.Client.FlushAll()
	end(err)
	return err
}
