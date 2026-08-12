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

package StarterRedigo

import (
	"context"
	"io"
	"net"

	"github.com/gomodule/redigo/redis"
	"go-spring.org/stdlib/errutil"
)

// driverRegistry maps driver names to their implementations. The bundled
// DefaultDriver is registered at init; custom drivers add themselves via
// RegisterDriver (e.g. from an init in the application).
var driverRegistry = map[string]Driver{}

func init() {
	RegisterDriver("DefaultDriver", DefaultDriver{})
}

// Driver interface defines how to create a Redis client (a connection pool).
// A company (or the bundled DefaultDriver) implements it once and registers
// via RegisterDriver; callers select one through Config.Driver, which defaults
// to "DefaultDriver".
//
// The returned io.Closer is the teardown for anything the driver built beyond
// the pool itself — for DefaultDriver that is the discovery resolver's
// background watch; a driver with nothing to clean up returns NopCloser().
// (*Pool).Close calls it on shutdown.
type Driver interface {
	CreateClient(ctx context.Context, c Config) (*redis.Pool, io.Closer, error)
}

// RegisterDriver registers a Redis driver with the given name.
// It panics if the driver name has already been registered.
func RegisterDriver(name string, driver Driver) {
	if _, ok := driverRegistry[name]; ok {
		panic("redis driver already registered: " + name)
	}
	driverRegistry[name] = driver
}

// DefaultDriver is the default implementation of the Driver interface.
type DefaultDriver struct{}

// CreateClient creates a new Redis pool based on the provided configuration.
//
// When c.ServiceName is set (and mesh mode is not enabled), the address is
// resolved through the registered discovery backend (c.Discovery) instead of
// c.Addr: a discovery.Resolver keeps the endpoint set fresh via a background
// watch and the pool dials a live instance (Pick) for each new connection.
// Combined with c.ConnMaxLifetime, pooled connections recycle onto updated
// addresses without rebuilding the pool. When c.ServiceName is empty this is a
// plain Addr dial, unchanged from before.
//
// In mesh mode (mesh.Enabled) discovery is skipped entirely: a sidecar owns
// discovery+LB, so the pool connects straight to the configured static Addr
// (the service's stable DNS address).
func (DefaultDriver) CreateClient(ctx context.Context, c Config) (*redis.Pool, io.Closer, error) {
	tlsConfig, err := c.TLS.Build()
	if err != nil {
		return nil, nil, errutil.Explain(err, "redis: build TLS")
	}

	resolver, err := newResolver(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	pool := &redis.Pool{
		MaxActive:       c.PoolSize,
		MaxIdle:         c.MaxIdle,
		MaxConnLifetime: c.ConnMaxLifetime,
		Wait:            true,
		Dial: func() (redis.Conn, error) {
			opts := []redis.DialOption{
				redis.DialPassword(c.Password),
				redis.DialConnectTimeout(c.DialTimeout),
				redis.DialReadTimeout(c.ReadTimeout),
				redis.DialWriteTimeout(c.WriteTimeout),
			}
			if c.Username != "" {
				opts = append(opts, redis.DialUsername(c.Username))
			}
			if tlsConfig != nil {
				opts = append(opts,
					redis.DialUseTLS(true),
					redis.DialTLSConfig(tlsConfig),
					redis.DialTLSSkipVerify(c.TLS.InsecureSkipVerify),
				)
			}
			// addr is the static target; with service discovery the resolver
			// overrides it by picking a live endpoint.
			addr := c.Addr
			if resolver != nil {
				nd := &net.Dialer{Timeout: c.DialTimeout}
				opts = append(opts, redis.DialContextFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
					ep, err := resolver.Pick()
					if err != nil {
						return nil, err
					}
					return nd.DialContext(ctx, network, ep.Addr)
				}))
				// Addr becomes a label for the pool; the dialer picks a live
				// endpoint.
				addr = c.ServiceName
			}
			conn, err := redis.Dial("tcp", addr, opts...)
			if err != nil {
				return nil, err
			}
			if c.DB != 0 {
				_, err = conn.Do("SELECT", c.DB)
				if err != nil {
					conn.Close()
					return nil, err
				}
			}
			return conn, nil
		},
	}
	// The driver owns the resolver's lifecycle: return its Stop as the teardown
	// closer so (*Pool).Close can stop the background watch without the starter
	// keeping a pool->resolver registry. No resolver (plain Addr / mesh) → no-op.
	stop := NopCloser()
	if resolver != nil {
		stop = closerFunc(resolver.Stop)
	}
	return pool, stop, nil
}
