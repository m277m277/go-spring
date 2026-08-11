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

package StarterNeo4j

import (
	"context"
	"runtime"
	"time"

	"go-spring.org/log"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/mesh"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-neo4j/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

func init() {
	// Register multiple Neo4j clients as a group, one per entry under
	// "${spring.neo4j}". A gs.Module (rather than gs.Group) is used so each
	// instance's neo4j.DriverWithContext bean can be paired with a health.Indicator
	// registered under the same name — and to attach the file:line of this
	// registration to the bean for diagnostics.
	_, file, line, _ := runtime.Caller(0)
	gs.Module(gs.OnProperty("spring.neo4j"), func(r gs.BeanProvider, p flatten.Storage) error {
		var m map[string]Config
		if err := conf.Bind(p, &m, "${spring.neo4j}"); err != nil {
			return err
		}
		for name, c := range m {
			// The wrapper bean owns the resilience executor + discovery watch, so
			// ApplyResilience arms it (InitMethod) and Close tears it down (Destroy).
			b := r.Provide(newClient,
				gs.IndexArg(1, gs.ValueArg(c)),
			).Name(name).InitMethod("ApplyResilience").Destroy((*ObservedNeo4jDriver).CloseDriver)
			b.SetFileLine(file, line)
			// Contribute a health indicator for this instance, injecting the
			// driver just registered above by name. The wrapper is what is
			// autowired; the embedded neo4j.DriverWithContext is handed to the
			// indicator.
			h := r.Provide(func(w *ObservedNeo4jDriver) health.Indicator {
				return health2.NewDriverHealth(name, w.DriverWithContext)
			}, gs.TagArg(name)).Name(name).Export(gs.As[health.Indicator]())
			h.SetFileLine(file, line)
		}
		return nil
	})
}

var starterTag = log.RegisterInfraTag("neo4j", "")

// newClient creates a new Neo4j client based on the provided configuration.
// After the driver is built, connectivity is verified so that misconfiguration
// or an unreachable server fails fast at startup rather than on first query.
//
// Observability note: the neo4j-go-driver speaks the binary Bolt protocol and
// ships no official OpenTelemetry instrumentation, nor a command-monitor hook
// comparable to the SQL/MongoDB drivers, so there is no clean seam to emit
// client spans from the starter. Rather than hand-roll a fragile bridge, tracing
// is left to the application (wrap ExecuteQuery / session calls with an OTel span
// where needed). This is a documented gap, not an oversight.
//
// When c.ServiceName is set and mesh mode is off, a Resolver is built against
// the registered discovery backend (c.Discovery), one endpoint is picked, and
// its address is spliced into the URI host. Because the neo4j driver exposes no
// dialer injection point, this is a one-shot resolution at startup — the
// Resolver is kept alive only to keep the lifecycle uniform with the other
// client starters and is stopped on shutdown. In mesh mode the sidecar owns
// discovery+LB, so the URI is used unchanged. See Config.ServiceName.
func newClient(ctx *gs.ContextProvider, c Config) (*ObservedNeo4jDriver, error) {
	log.Debugf(ctx.Context, starterTag, "creating neo4j client, uri=%s service-name=%s driver=%s", c.URI, c.ServiceName, c.Driver)

	var resolver *discovery.Resolver
	if c.ServiceName != "" && !mesh.Enabled() {
		uri, r, err := resolveURI(ctx.Context, c)
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "neo4j: resolve service-name failed: %v", err)
			return nil, err
		}
		resolver = r
		c.URI = uri
	}

	d, ok := driverRegistry[c.Driver]
	if !ok {
		log.Errorf(ctx.Context, starterTag, "neo4j driver not found: %s", c.Driver)
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(nil, "neo4j driver not found: %s", c.Driver)
	}
	client, err := d.CreateClient(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "neo4j: create client failed: %v", err)
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(err, "failed to create neo4j client")
	}

	w := &ObservedNeo4jDriver{DriverWithContext: client, cfg: c, resolver: resolver}
	// Fail fast: verify the server is reachable before handing out the driver.
	vctx, cancel := verifyContext(ctx.Context, c.SocketConnectTimeout)
	defer cancel()
	if err := client.VerifyConnectivity(vctx); err != nil {
		log.Errorf(ctx.Context, starterTag, "neo4j: verify connectivity failed uri=%s: %v", c.URI, err)
		_ = client.Close(ctx.Context)
		if resolver != nil {
			_ = resolver.Stop()
		}
		return nil, errutil.Explain(err, "failed to verify neo4j connectivity: %s", c.URI)
	}
	log.Infof(ctx.Context, starterTag, "neo4j client initialized, uri=%s", c.URI)
	return w, nil
}

// HealthCheck reports whether the Neo4j driver can reach the server. It is a
// thin readiness probe suitable for wiring into a health endpoint.
func HealthCheck(ctx context.Context, client *ObservedNeo4jDriver) error {
	return client.VerifyConnectivity(ctx)
}

// verifyContext derives a context for the startup connectivity check, bounded by
// the socket connect timeout when set so the probe cannot hang indefinitely.
func verifyContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
