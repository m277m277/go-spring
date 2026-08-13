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

package StarterGormClickhouse

import (
	"context"
	"net"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/mesh"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-gorm-clickhouse/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

var starterTag = log.RegisterInfraTag("gorm_clickhouse", "")

func init() {
	// Register multiple GORM clients as a group, one per entry under
	// "${spring.gorm.clickhouse}". A gs.Module (rather than gs.Group) is used so
	// each instance's *gorm.DB bean can be paired with a health.Indicator
	// registered under the same name — and to attach the file:line of this
	// registration to the bean for diagnostics.
	gs.Module(gs.OnProperty("spring.gorm.clickhouse"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.gorm.clickhouse}", func(name string, c Config) error {
			r.Provide(newClient, gs.IndexArg(1, gs.ValueArg(c))).Name(name).Init((*DB).Init).Destroy((*DB).Destroy).Caller(1)
			// Contribute a health indicator for this instance, injecting the
			// wrapper just registered above by name (the embedded *gorm.DB is
			// passed through to the indicator).
			r.Provide(func(w *DB) health.Indicator {
				return health2.NewGormHealth(name, w.DB)
			}, gs.TagArg(name)).Name("gorm:clickhouse:" + name).Export(gs.As[health.Indicator]()).Caller(1)
			return nil
		})
	})
}

// newClient creates a GORM database client using the ClickHouse driver, bridged
// into go-spring's unified observability. The otel plugin emits client spans and
// connection-pool metrics through the OTel globals that starter-otel installs;
// when starter-otel is absent those globals are no-ops, so this stays a
// zero-config, zero-overhead opt-in that needs no per-component adaptation.
//
// When c.ServiceName is set (and mesh mode is off), the connection is routed
// through a Resolver: the ClickHouse native driver builds a *sql.DB with our
// DialContext, so each new connection reaches a live instance resolved from the
// discovery backend and address changes take effect without rebuilding the
// client. In mesh mode a sidecar owns discovery+LB, so the configured Addr is
// used as-is. When c.ServiceName is empty this is a plain DSN dial, unchanged
// from before.
func newClient(ctx *gs.ContextProvider, c Config) (*DB, error) {
	if c.Addr == "" && c.ServiceName == "" {
		return nil, errutil.Explain(nil, "gorm clickhouse: one of addr or service-name must be set")
	}

	log.Debugf(ctx.Context, starterTag, "creating gorm clickhouse client, addr=%s service-name=%s db=%s", c.Addr, c.ServiceName, c.DB)

	var (
		db  *gorm.DB
		err error
		ld  *discovery.Resolver
	)

	// The native driver (ch.OpenDB) is required whenever we must inject a custom
	// TLS config or a discovery-backed dialer, neither of which the URL-style DSN
	// can express. Otherwise the plain DSN path stays as before. Mesh mode skips
	// discovery (sidecar owns it) but may still need native for TLS.
	useDiscovery := c.ServiceName != "" && !mesh.Enabled()
	useNative := useDiscovery || c.TLS.Enabled
	if !useNative {
		db, err = gorm.Open(clickhouse.Open(c.DSN()), gormConfig(c))
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm clickhouse: open failed: %v", err)
			return nil, err
		}
	} else {
		opts := &ch.Options{
			Addr: []string{c.Addr},
			Auth: ch.Auth{
				Database: c.DB,
				Username: c.User,
				Password: c.Password,
			},
			DialTimeout: c.DialTimeout,
			ReadTimeout: c.ReadTimeout,
		}
		if c.TLS.Enabled {
			tlsCfg, terr := c.TLS.Build()
			if terr != nil {
				log.Errorf(ctx.Context, starterTag, "gorm clickhouse: build TLS failed: %v", terr)
				return nil, errutil.Explain(terr, "gorm-clickhouse: build TLS")
			}
			opts.TLS = tlsCfg
		}
		if useDiscovery {
			var derr error
			ld, derr = newLiveResolver(ctx.Context, c)
			if derr != nil {
				log.Errorf(ctx.Context, starterTag, "gorm clickhouse: build discovery resolver failed: %v", derr)
				return nil, derr
			}
			// ch.Options.DialContext is 2-arg: func(ctx, addr string) (net.Conn, error).
			// The addr is ignored; the dialer picks a live endpoint via the Resolver.
			nd := &net.Dialer{}
			opts.DialContext = func(ctx context.Context, _ string) (net.Conn, error) {
				ep, perr := ld.Pick()
				if perr != nil {
					return nil, perr
				}
				return nd.DialContext(ctx, "tcp", ep.Addr)
			}
		}
		sqlDB := ch.OpenDB(opts)
		db, err = gorm.Open(clickhouse.New(clickhouse.Config{Conn: sqlDB}), gormConfig(c))
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm clickhouse: open with native driver failed: %v", err)
			if ld != nil {
				_ = ld.Stop()
			}
			_ = sqlDB.Close()
			return nil, err
		}
	}
	// Fail fast: verify connectivity and apply pool settings at creation time.
	// The observe plugin + resilience callbacks are installed later by the
	// wrapper's Init (InitMethod), after gs field-injects its
	// Observability + Resilience config.
	if err := applyPool(db, c); err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm clickhouse: ping failed: %v", err)
		if ld != nil {
			_ = ld.Stop()
		}
		if sqlDB, derr := db.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	if ld != nil {
		liveDialers.Store(db, ld)
	}
	if err := applyDBCustomizers(c, db); err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm clickhouse: customizer failed: %v", err)
		if ld != nil {
			_ = ld.Stop()
		}
		if sqlDB, derr := db.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	log.Infof(ctx.Context, starterTag, "gorm clickhouse client initialized, addr=%s db=%s", c.Addr, c.DB)
	return &DB{DB: db, cfg: c}, nil
}
