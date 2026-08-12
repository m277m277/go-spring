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

package StarterGormMySql

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
	"go-spring.org/cloud/actuator/health"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	health2 "go-spring.org/starter-gorm-mysql/health"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// tlsConfigs tracks the custom TLS config name registered with the mysql driver
// for a client, so the wrapper's Close can deregister it on teardown.
var tlsConfigs sync.Map // *gorm.DB -> string (tls config name)

// tlsSeq makes each registered custom TLS config name unique.
var tlsSeq atomic.Uint64

var starterTag = log.RegisterInfraTag("gorm_mysql", "")

func init() {
	// Register multiple GORM clients as a group, one per entry under
	// "${spring.gorm.mysql}". A gs.Module (rather than gs.Group) is used so each
	// instance's *gorm.DB bean can be paired with a health.Indicator registered
	// under the same name — and to attach the file:line of this registration to
	// the bean for diagnostics.
	gs.Module(gs.OnProperty("spring.gorm.mysql"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.gorm.mysql}", func(name string, c Config) error {
			r.Provide(newClient, gs.IndexArg(1, gs.ValueArg(c))).Name(name).Init((*DB).Init).Destroy((*DB).Destroy).Caller(1)
			// Contribute a health indicator for this instance, injecting the
			// wrapper just registered above by name (the embedded *gorm.DB is
			// passed through to the indicator).
			r.Provide(func(w *DB) health.Indicator {
				return health2.NewGormHealth(name, w.DB)
			}, gs.TagArg(name)).Name("gorm:mysql:" + name).Export(gs.As[health.Indicator]()).Caller(1)
			return nil
		})
	})
}

// newClient creates a GORM database client using the MySQL driver, bridged into
// go-spring's unified observability. The otel plugin emits client spans and
// connection-pool metrics through the OTel globals that starter-otel installs;
// when starter-otel is absent those globals are no-ops, so this stays a
// zero-config, zero-overhead opt-in that needs no per-component adaptation.
//
// When c.ServiceName is set (and mesh mode is off), the address is resolved
// through the registered discovery backend: a Resolver is bound to a unique
// mysql dial network name and the DSN routes through it, so each new connection
// reaches a live instance and address changes take effect without rebuilding
// the client. In mesh mode a sidecar owns discovery+LB, so the configured Addr
// is used as-is. When c.ServiceName is empty this is a plain Addr dial,
// unchanged from before.
func newClient(ctx *gs.ContextProvider, c Config) (*DB, error) {

	if c.Addr == "" && c.ServiceName == "" {
		return nil, fmt.Errorf("gorm mysql: one of addr or service-name must be set")
	}

	log.Debugf(ctx.Context, starterTag, "creating gorm mysql client, addr=%s service-name=%s db=%s", c.Addr, c.ServiceName, c.DB)

	// Resolve the TLS DSN parameter. The shared TLS builder returns a fully
	// materialized *tls.Config when TLS is enabled (empty CAFile falls back to
	// the host's system root set; ServerName/InsecureSkipVerify honored), or
	// (nil, nil) when disabled. Register the config with the driver under a
	// unique name and reference it in the DSN as tls=<name>.
	var tlsName string
	tlsCfg, err := c.TLS.Build()
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm mysql: build TLS failed: %v", err)
		return nil, errutil.Explain(err, "gorm-mysql: build TLS")
	}
	if tlsCfg != nil {
		tlsName = fmt.Sprintf("gstls_%d", tlsSeq.Add(1))
		if err := mysql.RegisterTLSConfig(tlsName, tlsCfg); err != nil {
			return nil, err
		}
		c.tlsParam = tlsName
	}

	dsn := c.DSN()

	conn, err := newDiscoveryConn(ctx.Context, c)
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm mysql: build discovery dialer failed: %v", err)
		deregisterTLS(tlsName)
		return nil, err
	}
	if conn != nil {
		// Route the DSN through the registered dialer; Addr becomes a label the
		// dialer ignores since it picks a live endpoint itself.
		dc := c
		dc.Network = conn.netName
		dc.Addr = c.ServiceName
		dsn = dc.DSN()
	}

	db, err := gorm.Open(gormmysql.Open(dsn), gormConfig(c))
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm mysql: open failed: %v", err)
		cleanup(conn, tlsName)
		return nil, err
	}
	// Fail fast: verify connectivity and apply pool settings at creation time.
	// The observe plugin + resilience callbacks are installed later by the
	// wrapper's Init (InitMethod), after gs field-injects its
	// Observability + Resilience config.
	if err := applyPool(db, c); err != nil {
		log.Errorf(ctx.Context, starterTag, "gorm mysql: ping failed: %v", err)
		cleanup(conn, tlsName)
		if sqlDB, derr := db.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	if conn != nil {
		liveDialers.Store(db, conn)
	}
	if tlsName != "" {
		tlsConfigs.Store(db, tlsName)
	}
	log.Infof(ctx.Context, starterTag, "gorm mysql client initialized, addr=%s db=%s", c.Addr, c.DB)
	return &DB{DB: db, cfg: c}, nil
}

// cleanup stops a discovery dialer and deregisters driver-scoped names created
// during a failed newClient attempt.
func cleanup(conn *discoveryConn, tlsName string) {
	stopDiscoveryConn(conn)
	deregisterTLS(tlsName)
}

// deregisterTLS removes a custom TLS config previously registered with the
// mysql driver. It is a no-op for the empty name (built-in modes).
func deregisterTLS(name string) {
	if name != "" {
		mysql.DeregisterTLSConfig(name)
	}
}
