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

package StarterGormPostgres

import (
	"context"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/cloud/discovery"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-gorm-postgres/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var starterTag = log.RegisterInfraTag("gorm_postgres", "")

func init() {
	// Register multiple GORM clients as a group, one per entry under
	// "${spring.gorm.postgres}". A gs.Module (rather than gs.Group) is used so
	// each instance's *gorm.DB bean can be paired with a health.Indicator
	// registered under the same name — and to attach the file:line of this
	// registration to the bean for diagnostics.
	gs.Module(gs.OnProperty("spring.gorm.postgres"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.gorm.postgres}", func(name string, c Config) error {
			r.Provide(newClient, gs.IndexArg(1, gs.ValueArg(c))).Name(name).Init((*DB).Init).Destroy((*DB).Destroy).Caller(1)
			// Contribute a health indicator for this instance, injecting the
			// wrapper just registered above by name (the embedded *gorm.DB is
			// passed through to the indicator).
			r.Provide(func(w *DB) health.Indicator {
				return health2.NewGormHealth(name, w.DB)
			}, gs.TagArg(name)).Name("gorm:postgres:" + name).Export(gs.As[health.Indicator]()).Caller(1)
			return nil
		})
	})
}

// newClient creates a GORM database client using the PostgreSQL driver, bridged
// into go-spring's unified observability. The otel plugin emits client spans and
// connection-pool metrics through the OTel globals that starter-otel installs;
// when starter-otel is absent those globals are no-ops, so this stays a
// zero-config, zero-overhead opt-in that needs no per-component adaptation.
//
// When c.ServiceName is set (and mesh mode is off), the address is resolved
// through the registered discovery backend: a Resolver is bound to the pgx
// DialFunc so each new physical connection reaches a live instance and address
// changes take effect without rebuilding the client. In mesh mode a sidecar
// owns discovery+LB, so the configured Host is used as-is. When c.ServiceName
// is empty this is a plain DSN dial, unchanged from before.
func newClient(ctx *gs.ContextProvider, c Config) (*DB, error) {
	if c.Host == "" && c.ServiceName == "" {
		return nil, errutil.Explain(nil, "gorm postgres: one of host or service-name must be set")
	}

	log.Debugf(ctx.Context, starterTag, "creating gorm postgres client, host=%s service-name=%s db=%s", c.Host, c.ServiceName, c.DB)

	var (
		db  *gorm.DB
		err error
		ld  *discovery.Resolver
	)

	ld, err = newLiveResolver(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm postgres: build discovery resolver failed: %v", err)
		return nil, err
	}
	if ld != nil {
		pgxCfg, err := pgx.ParseConfig(c.DSN())
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm postgres: parse pgx config failed: %v", err)
			_ = ld.Stop()
			return nil, err
		}
		// pgconn.DialFunc is 3-arg: func(ctx, network, addr string) (net.Conn, error).
		// Both network and addr are ignored; the dialer picks a live endpoint via
		// the Resolver and dials it over TCP.
		nd := &net.Dialer{}
		pgxCfg.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
			ep, perr := ld.Pick()
			if perr != nil {
				return nil, perr
			}
			return nd.DialContext(ctx, "tcp", ep.Addr)
		}
		sqlDB := stdlib.OpenDB(*pgxCfg)
		db, err = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), gormConfig(c))
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm postgres: open with discovery failed: %v", err)
			_ = sqlDB.Close()
			_ = ld.Stop()
			return nil, err
		}
	} else {
		// Plain DSN mode (no discovery): dial the configured host:port directly.
		db, err = gorm.Open(postgres.New(postgres.Config{DSN: c.DSN()}), gormConfig(c))
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm postgres: open failed: %v", err)
			return nil, err
		}
	}

	// Fail fast: verify connectivity and apply pool settings at creation time.
	// The observe plugin + resilience callbacks are installed later by the
	// wrapper's Init (InitMethod), after gs field-injects its
	// Observability + Resilience config.
	if err := applyPool(db, c); err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm postgres: ping failed: %v", err)
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
		log.Errorf(ctx.Context, starterTag, "gorm postgres: customizer failed: %v", err)
		if ld != nil {
			_ = ld.Stop()
		}
		if sqlDB, derr := db.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	log.Infof(ctx.Context, starterTag, "gorm postgres client initialized, host=%s db=%s", c.Host, c.DB)
	return &DB{DB: db, cfg: c}, nil
}
