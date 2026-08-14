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

// wrapper.go is the DB entity concept of this starter — the DB wrapper gorm
// clients are injected as, plus its lifecycle (Init/Destroy), resource label,
// and the discovery-backed dialer that owns live-instance routing. The entity
// embeds the concrete *gorm.DB and owns the resilience executor + the teardown
// closer (stopping the discovery watch and deregistering the dialer), while the
// DB-construction helpers live in db.go and the post-open extension seam in
// extension.go.
package StarterGormMySql

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/governance/fault"
	"go-spring.org/cloud/governance/resilience"
	observe "go-spring.org/cloud/observe"
	"go-spring.org/cloud/observe/resilience"
	gormobserve "go-spring.org/starter-gorm/observe"
	"go-spring.org/starter-gorm/resilience"
	"gorm.io/gorm"
)

// DB is the wrapper bean gorm-mysql clients are injected as. It
// embeds *gorm.DB so all gorm methods promote unchanged, and field-injects
// Observability. newClient returns one; gs field-injects Observability, then
// calls Init (InitMethod) to install the observe plugin and the resilience
// callbacks. Both resilience and fault are resolved through neutral seams
// ([resilience.ExecutorFor] / [fault.InjectorFor]) backed by starter-govern's
// governance center — so this struct has zero coupling to cloud/governance.
type DB struct {
	*gorm.DB
	Observability observe.ObserveConfig `value:"${observability:=}"`

	cfg      Config              // for resourceLabel (addr/service-name)
	exec     resilience.Executor // resolved via resilience.ExecutorFor; no-op when governance is off
	resource string
}

// Init is the gs InitMethod (runs after gs field-injects Observability). It
// installs the shared gorm observe plugin, resolves the resilience executor
// through the neutral [resilience.ExecutorFor] seam (backed by starter-govern's
// governance center when configured; a transparent no-op otherwise), wraps it
// with the process-wide fault injector ([fault.InjectorFor], nil-safe), and
// routes every gorm processor through it via [gormresilience.ApplyCallbacks].
func (o *DB) Init() error {
	if o.cfg.ObserveEnabled {
		if err := o.DB.Use(gormobserve.NewPlugin("mysql", o.Observability)); err != nil {
			return err
		}
	}
	o.resource = resilience.ResourceLabel("gorm:mysql", o.cfg.ServiceName, o.cfg.Addr)
	exec := fault.WrapExecutor(resilience.ExecutorFor(o.resource), fault.InjectorFor())
	exec = resilobserve.WrapExecutor(exec, "mysql", o.Observability)
	o.exec = exec
	if err := gormresilience.ApplyCallbacks(o.DB, exec, o.resource); err != nil {
		return err
	}
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor,
// stops any discovery dialer watch and deregisters the TLS config behind the
// client, then closes the underlying connection pool.
func (o *DB) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	if v, ok := liveDialers.LoadAndDelete(o.DB); ok {
		stopDiscoveryConn(v.(*discoveryConn))
	}
	if v, ok := tlsConfigs.LoadAndDelete(o.DB); ok {
		mysql.DeregisterTLSConfig(v.(string))
	}
	if sqlDB, err := o.DB.DB(); err == nil {
		return sqlDB.Close()
	}
	return nil
}

// liveDialers tracks the discovery-backed dialer and its registered network
// name behind each client, so the wrapper's Close can stop the watch and
// deregister.
var liveDialers sync.Map // *gorm.DB -> *discoveryConn

// netSeq makes each registered mysql dial network name unique, so multiple
// instances discovering the same service never collide.
var netSeq atomic.Uint64

// discoveryConn pairs a live Resolver with the unique mysql dial network name
// it registered, so the Close-half can stop the watch and deregister the dialer.
type discoveryConn struct {
	ld      *discovery.Resolver
	netName string
}

// newDiscoveryConn resolves the registered discovery backend for c and registers
// a mysql dialer that routes each new connection through a live endpoint. It
// returns (nil, nil) when service-name is unset or mesh mode is enabled (a
// sidecar owns discovery+LB), in which case the caller dials the configured Addr
// directly. The caller owns the lifecycle and must release the conn via
// stopDiscoveryConn.
func newDiscoveryConn(ctx context.Context, c Config) (*discoveryConn, error) {
	ld, err := discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
	if err != nil {
		return nil, err
	}
	if ld == nil {
		return nil, nil
	}
	netName := fmt.Sprintf("gsdisco_%s_%d", c.ServiceName, netSeq.Add(1))
	nd := &net.Dialer{}
	// mysql.DialContextFunc is 2-arg: func(ctx, addr string) (net.Conn, error).
	// The addr is ignored; the dialer picks a live endpoint via the Resolver.
	mysql.RegisterDialContext(netName, func(ctx context.Context, _ string) (net.Conn, error) {
		ep, perr := ld.Pick()
		if perr != nil {
			return nil, perr
		}
		return nd.DialContext(ctx, "tcp", ep.Addr)
	})
	return &discoveryConn{ld: ld, netName: netName}, nil
}

// stopDiscoveryConn stops the discovery watch and deregisters the mysql dialer
// behind conn. It is the Close-half of the discovery lifecycle, symmetric with
// newDiscoveryConn; it is a no-op for a nil conn (a client that never had one).
func stopDiscoveryConn(conn *discoveryConn) {
	if conn == nil {
		return
	}
	_ = conn.ld.Stop()
	mysql.DeregisterDialContext(conn.netName)
}
