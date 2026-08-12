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
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
	resilobserve "go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
)

// Client wraps *memcache.Client so every operation flows through the
// shared observe kit (trace span + duration/in-flight metric + access log). It
// embeds the real client, so methods not overridden below (only Close, a
// lifecycle method) are promoted unchanged. memcache's API carries no context,
// so spans are root spans (not linked to the caller's request trace) — a
// limitation of gomemcache, documented here.
//
// The type is exported because gomemcache (unlike go-redis or gorm) offers no
// hook/plugin extension point, so the only way to observe per-operation traffic
// is to hold the wrapper itself. Apps therefore inject *Client rather
// than *memcache.Client; the embedded field is available for any third-party
// API that needs the raw client.
type Client struct {
	*memcache.Client
	obs *observe.Observer

	// Resilience is field-injected (gs.Dync, hot-reloadable) so the protection
	// policy can change at runtime without a restart; Init subscribes
	// OnChanged to Refresh the executor. Observability is the startup access-log
	// config (not hot). These replace the old Config.Resilience/Observability
	// fields so the wrapper bean owns its own protection policy.
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	// name is the instance name (the spring.memcached.<name> map key), used for
	// the resilience resource label. Set by newClient; Init reads it.
	name string

	// exec is the resilience executor protecting every operation, set by
	// Init when resilience is enabled. nil on an unarmed client, in
	// which case guard runs the operation directly with no policy overhead.
	exec resilience.Executor
	// resource is the resilience resource key ("memcached:<instance-name>")
	// exec scopes limiter/breaker state by. Only meaningful when exec != nil.
	resource string
}

// Init is the gs InitMethod: gs field-injects Resilience + Observability
// after newClient returns, then calls this. It builds the observe.Observer (needs
// Observability) and, when resilience is enabled, the executor (needs the
// Resilience policy) + arms guard + subscribes to policy changes for hot Refresh.
func (c *Client) Init() error {
	c.obs = observe.NewClient("memcached", c.Observability)
	rc := c.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	exec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "memcached", c.Observability)
	c.exec = exec
	c.resource = resilience.ResourceLabel("memcached", c.name)
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh resets per-resource state (the intended semantic
	// of a threshold change - old failure counts were under the old policy).
	c.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}

// Close releases the resilience executor (if armed) and stops any discovery
// Resolver watch behind the client. The memcache client itself keeps a lazy
// connection pool with no Close method, so only the background watch and the
// executor's resources are released here. It is the gs destroy method.
func (c *Client) Destroy() error {
	if c.exec != nil {
		_ = c.exec.Close()
	}
	stopLiveResolver(c.Client)
	return nil
}

// instrument wraps a single-key operation: op names it, key is the log/span arg.
// It starts an observe span and returns an end callback the caller invokes with
// the operation's error. The resilience executor is applied by the caller via
// guard (see resilience.go), so each method is span-instrumented here and
// resilience-protected there — two single places rather than 17 copies of each.
func (c *Client) instrument(op, key string) func(error) {
	_, sp := c.obs.Start(context.Background(), op, key)
	return func(err error) { sp.End(err) }
}

func (c *Client) Get(key string) (*memcache.Item, error) {
	end := c.instrument("get", key)
	it, err := guard(c.exec, c.resource, func() (*memcache.Item, error) { return c.Client.Get(key) })
	end(err)
	return it, err
}

func (c *Client) GetAndTouch(key string, expiration int32) (*memcache.Item, error) {
	end := c.instrument("get_and_touch", key)
	it, err := guard(c.exec, c.resource, func() (*memcache.Item, error) {
		return c.Client.GetAndTouch(key, expiration)
	})
	end(err)
	return it, err
}

func (c *Client) GetMulti(keys []string) (map[string]*memcache.Item, error) {
	end := c.instrument("get_multi", fmt.Sprintf("%d keys", len(keys)))
	m, err := guard(c.exec, c.resource, func() (map[string]*memcache.Item, error) {
		return c.Client.GetMulti(keys)
	})
	end(err)
	return m, err
}

func (c *Client) Touch(key string, seconds int32) error {
	end := c.instrument("touch", key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Touch(key, seconds) })
	end(err)
	return err
}

func (c *Client) Set(item *memcache.Item) error {
	end := c.instrument("set", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Set(item) })
	end(err)
	return err
}

func (c *Client) Add(item *memcache.Item) error {
	end := c.instrument("add", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Add(item) })
	end(err)
	return err
}

func (c *Client) Replace(item *memcache.Item) error {
	end := c.instrument("replace", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Replace(item) })
	end(err)
	return err
}

func (c *Client) Append(item *memcache.Item) error {
	end := c.instrument("append", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Append(item) })
	end(err)
	return err
}

func (c *Client) Prepend(item *memcache.Item) error {
	end := c.instrument("prepend", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Prepend(item) })
	end(err)
	return err
}

func (c *Client) CompareAndSwap(item *memcache.Item) error {
	end := c.instrument("cas", item.Key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.CompareAndSwap(item) })
	end(err)
	return err
}

func (c *Client) Delete(key string) error {
	end := c.instrument("delete", key)
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Delete(key) })
	end(err)
	return err
}

func (c *Client) DeleteAll() error {
	end := c.instrument("delete_all", "")
	err := guardErr(c.exec, c.resource, func() error { return c.Client.DeleteAll() })
	end(err)
	return err
}

func (c *Client) Increment(key string, delta uint64) (uint64, error) {
	end := c.instrument("increment", key)
	n, err := guard(c.exec, c.resource, func() (uint64, error) { return c.Client.Increment(key, delta) })
	end(err)
	return n, err
}

func (c *Client) Decrement(key string, delta uint64) (uint64, error) {
	end := c.instrument("decrement", key)
	n, err := guard(c.exec, c.resource, func() (uint64, error) { return c.Client.Decrement(key, delta) })
	end(err)
	return n, err
}

func (c *Client) Ping() error {
	end := c.instrument("ping", "")
	err := guardErr(c.exec, c.resource, func() error { return c.Client.Ping() })
	end(err)
	return err
}

func (c *Client) FlushAll() error {
	end := c.instrument("flush_all", "")
	err := guardErr(c.exec, c.resource, func() error { return c.Client.FlushAll() })
	end(err)
	return err
}
