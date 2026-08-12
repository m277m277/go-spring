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

package StarterGormSqlserver

import (
	"database/sql"
	"net"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-gorm-sqlserver/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

var starterTag = log.RegisterInfraTag("gorm_sqlserver", "")

func init() {
	// Register multiple GORM clients as a group, one per entry under
	// "${spring.gorm.sqlserver}". A gs.Module (rather than gs.Group) is used so
	// each instance's *gorm.DB bean can be paired with a health.Indicator
	// registered under the same name — and to attach the file:line of this
	// registration to the bean for diagnostics.
	gs.Module(gs.OnProperty("spring.gorm.sqlserver"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.gorm.sqlserver}", func(name string, c Config) error {
			r.Provide(newClient, gs.IndexArg(1, gs.ValueArg(c))).Name(name).Init((*DB).Init).Destroy((*DB).Destroy).Caller(1)
			// Contribute a health indicator for this instance, injecting the
			// wrapper just registered above by name (the embedded *gorm.DB is
			// passed through to the indicator).
			r.Provide(func(w *DB) health.Indicator {
				return health2.NewGormHealth(name, w.DB)
			}, gs.TagArg(name)).Name("gorm:sqlserver:" + name).Export(gs.As[health.Indicator]()).Caller(1)
			return nil
		})
	})
}

// newClient creates a GORM database client using the SQL Server driver, bridged
// into go-spring's unified observability. The otel plugin emits client spans and
// connection-pool metrics through the OTel globals that starter-otel installs;
// when starter-otel is absent those globals are no-ops, so this stays a
// zero-config, zero-overhead opt-in that needs no per-component adaptation.
//
// When c.ServiceName is set (and mesh mode is off), the connection is routed
// through a Resolver that resolves the service name against the configured
// discovery backend on every dial. The mssql Connector.Dialer hook accepts our
// resolverDialer adapter, which implements mssql.Dialer. In mesh mode a sidecar
// owns discovery+LB, so the configured Host is used as-is. When c.ServiceName
// is empty this stays a plain DSN dial, unchanged from before.
func newClient(ctx *gs.ContextProvider, c Config) (*DB, error) {
	if c.Host == "" && c.ServiceName == "" {
		return nil, errutil.Explain(nil, "gorm sqlserver: one of host or service-name must be set")
	}

	log.Debugf(ctx.Context, starterTag, "creating gorm sqlserver client, host=%s service-name=%s db=%s", c.Host, c.ServiceName, c.DB)

	ld, err := newLiveResolver(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm sqlserver: build discovery resolver failed: %v", err)
		return nil, err
	}
	if ld != nil {
		msCfg, err := msdsn.Parse(c.DSN())
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm sqlserver: parse DSN failed: %v", err)
			_ = ld.Stop()
			return nil, err
		}
		connector := mssql.NewConnectorConfig(msCfg)
		connector.Dialer = resolverDialer{r: ld, nd: &net.Dialer{}}
		sqlDB := sql.OpenDB(connector)

		db, err := gorm.Open(sqlserver.New(sqlserver.Config{Conn: sqlDB}), gormConfig(c))
		if err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm sqlserver: open with discovery failed: %v", err)
			_ = ld.Stop()
			_ = sqlDB.Close()
			return nil, err
		}
		// Fail fast: verify connectivity and apply pool settings at creation time.
		// The observe plugin + resilience callbacks are installed later by the
		// wrapper's Init (InitMethod).
		if err := applyPool(db, c); err != nil {
			log.Errorf(ctx.Context, starterTag, "gorm sqlserver: ping failed: %v", err)
			_ = ld.Stop()
			_ = sqlDB.Close()
			return nil, err
		}
		liveDialers.Store(db, ld)
		log.Infof(ctx.Context, starterTag, "gorm sqlserver client initialized, service-name=%s db=%s", c.ServiceName, c.DB)
		return &DB{DB: db, cfg: c}, nil
	}

	db, err := gorm.Open(sqlserver.Open(c.DSN()), gormConfig(c))
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm sqlserver: open failed: %v", err)
		return nil, err
	}
	// Fail fast: verify connectivity and apply pool settings at creation time.
	// The observe plugin + resilience callbacks are installed later by the
	// wrapper's Init (InitMethod).
	if err := applyPool(db, c); err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm sqlserver: ping failed: %v", err)
		if sqlDB, derr := db.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	log.Infof(ctx.Context, starterTag, "gorm sqlserver client initialized, host=%s db=%s", c.Host, c.DB)
	return &DB{DB: db, cfg: c}, nil
}
