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

package StarterMongoDB

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"time"

	"go-spring.org/log"
	observe "go-spring.org/observe"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-mongodb/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var starterTag = log.RegisterInfraTag("mongodb", "")

// dialerWrapper adapts a dial function (plain, discovery-backed, or
// resilience-wrapped) to the mongo driver's options.Dialer interface. The
// dialed address is taken as-is so the underlying function (which may itself
// ignore it in favor of a Resolver pick) decides the target.
type dialerWrapper struct {
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

func (d *dialerWrapper) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dial(ctx, network, address)
}

func init() {
	// Register multiple MongoDB clients as a group, one per entry under
	// "${spring.mongodb}". A gs.Module (rather than gs.Group) is used so each
	// instance's *mongo.Client bean can be paired with a health.Indicator
	// registered under the same name — and to attach the file:line of this
	// registration to the bean for diagnostics.
	_, file, line, _ := runtime.Caller(0)
	gs.Module(gs.OnProperty("spring.mongodb"), func(r gs.BeanProvider, p flatten.Storage) error {
		var m map[string]Config
		if err := conf.Bind(p, &m, "${spring.mongodb}"); err != nil {
			return err
		}
		for name, c := range m {
			// The wrapper bean owns the resilience executor + discovery watch, so
			// ApplyResilience arms it (InitMethod) and Close tears it down (Destroy).
			b := r.Provide(newClient,
				gs.IndexArg(1, gs.ValueArg(c)),
			).Name(name).InitMethod("ApplyResilience").Destroy((*ObservedMongoClient).Close)
			b.SetFileLine(file, line)
			// Contribute a health indicator for this instance, injecting the
			// client just registered above by name. The wrapper is what is
			// autowired; the embedded *mongo.Client is handed to the indicator.
			h := r.Provide(func(w *ObservedMongoClient) health.Indicator {
				return health2.NewClientHealth(name, w.Client)
			}, gs.TagArg(name)).Name(name).Export(gs.As[health.Indicator]())
			h.SetFileLine(file, line)
		}
		return nil
	})
}

// newClient creates a new MongoDB client based on the provided configuration,
// wrapped so gs can field-inject resilience + observability and
// ApplyResilience (InitMethod) can arm them. The command monitor (observability)
// and the dial seam (resilience) are installed dynamically: newClient wires a
// mutable monitor + dialer into the driver, and ApplyResilience later swaps in
// the observe observer and the resilience-wrapped dial function once the
// injected policy is available. After the client is built it is pinged so that
// misconfiguration or an unreachable server fails fast at startup rather than
// on first use.
//
// When c.ServiceName is set and mesh mode is off, the address is resolved
// through the registered discovery backend (c.Discovery): a Resolver-backed
// dialer is injected as the client's ContextDialer, so each new connection
// dials a currently-live instance picked by the Resolver and address changes
// take effect without rebuilding the client. In mesh mode a sidecar owns
// discovery+LB, so the URI hosts are dialed directly. When c.ServiceName is
// empty this dials the URI hosts directly, unchanged from before.
func newClient(ctx *gs.ContextProvider, c Config) (*ObservedMongoClient, error) {
	log.Debugf(ctx.Context, starterTag, "creating mongodb client, uri=%s service-name=%s", c.URI, c.ServiceName)

	opts := options.Client().ApplyURI(c.URI)
	if c.ConnectTimeout > 0 {
		opts.SetConnectTimeout(c.ConnectTimeout)
	}
	if c.ServerSelectionTimeout > 0 {
		opts.SetServerSelectionTimeout(c.ServerSelectionTimeout)
	}
	if c.MaxPoolSize > 0 {
		opts.SetMaxPoolSize(c.MaxPoolSize)
	}
	opts.SetMinPoolSize(c.MinPoolSize)
	if c.MaxConnIdleTime > 0 {
		opts.SetMaxConnIdleTime(c.MaxConnIdleTime)
	}
	if c.Username != "" {
		opts.SetAuth(options.Credential{
			Username:      c.Username,
			Password:      c.Password,
			AuthSource:    c.AuthSource,
			AuthMechanism: c.AuthMechanism,
		})
	}
	tlsCfg, err := c.TLS.Build()
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "mongodb: build TLS failed: %v", err)
		return nil, errutil.Explain(err, "mongodb: build TLS")
	}
	if tlsCfg != nil {
		opts.SetTLSConfig(tlsCfg)
	}

	w := &ObservedMongoClient{cfg: c}
	// The command monitor observes operations; it reads the observer lazily so
	// ApplyResilience can build it from the injected Observability config once
	// the wrapper is field-injected. No commands run before ApplyResilience.
	opts.SetMonitor(newCommandMonitor(func() *observe.Observer { return w.obs.Load() }))

	var baseDial func(ctx context.Context, network, address string) (net.Conn, error)
	w.resolver, err = newLiveResolver(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "mongodb: build discovery resolver failed: %v", err)
		return nil, err
	}
	if w.resolver != nil {
		nd := &net.Dialer{Timeout: c.ConnectTimeout}
		// The discovery dialer ignores the URI address and picks a live
		// endpoint via the Resolver on each new connection.
		pick := w.resolver.Pick
		baseDial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			ep, err := pick()
			if err != nil {
				return nil, err
			}
			return nd.DialContext(ctx, network, ep.Addr)
		}
	}
	if baseDial == nil {
		nd := &net.Dialer{Timeout: c.ConnectTimeout}
		baseDial = nd.DialContext
	}
	// A shared dialer instance is handed to the driver; ApplyResilience mutates
	// its dial field (wrapping it with resilience.NewDialer) so the swap takes
	// effect without rebuilding the client.
	w.dialer = &dialerWrapper{dial: baseDial}
	opts.SetDialer(w.dialer)

	client, err := mongo.Connect(opts)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "mongodb: connect failed: %v", err)
		if w.resolver != nil {
			_ = w.resolver.Stop()
		}
		return nil, fmt.Errorf("mongodb: create client: %w", err)
	}
	w.Client = client

	// Fail fast: verify the server is reachable before handing out the client.
	pingCtx, cancel := pingContext(ctx.Context, c.ConnectTimeout)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		log.Errorf(ctx.Context, starterTag, "mongodb: ping failed uri=%s: %v", c.URI, err)
		_ = client.Disconnect(context.Background())
		if w.resolver != nil {
			_ = w.resolver.Stop()
		}
		return nil, fmt.Errorf("mongodb: ping %s: %w", c.URI, err)
	}
	log.Infof(ctx.Context, starterTag, "mongodb client initialized, uri=%s", c.URI)
	return w, nil
}

// HealthCheck reports whether the MongoDB client can reach the server. It is a
// thin readiness probe suitable for wiring into a health endpoint.
func HealthCheck(ctx context.Context, client *ObservedMongoClient) error {
	return client.Ping(ctx, nil)
}

// pingContext derives a context for the startup ping, bounded by the connect
// timeout when set so the probe cannot hang indefinitely.
func pingContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
