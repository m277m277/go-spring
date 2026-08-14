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
// and the discovery-backed resolver that owns live-instance routing. The entity
// embeds the concrete *gorm.DB and owns the resilience executor + the teardown
// closer (stopping the discovery watch), while the DB-construction helpers live
// in db.go and the post-open extension seam in extension.go.
package StarterGormPostgres

import (
	"context"
	"sync"

	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/governance/fault"
	"go-spring.org/cloud/governance/resilience"
	observe "go-spring.org/cloud/observe"
	"go-spring.org/cloud/observe/resilience"
	gormobserve "go-spring.org/starter-gorm/observe"
	"go-spring.org/starter-gorm/resilience"
	"gorm.io/gorm"
)

// DB is the wrapper bean gorm-postgres clients are injected as. It
// embeds *gorm.DB so all gorm methods promote unchanged, and field-injects
// Observability. newClient returns one; gs field-injects Observability, then
// calls Init (InitMethod) to install the observe plugin and the resilience
// callbacks. Both resilience and fault are resolved through neutral seams
// ([resilience.ExecutorFor] / [fault.InjectorFor]) backed by starter-govern's
// governance center — so this struct has zero coupling to cloud/governance.
type DB struct {
	*gorm.DB
	Observability observe.ObserveConfig `value:"${observability:=}"`

	cfg      Config              // for resourceLabel (host/service-name)
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
		if err := o.DB.Use(gormobserve.NewPlugin("postgresql", o.Observability)); err != nil {
			return err
		}
	}
	o.resource = resilience.ResourceLabel("gorm:postgresql", o.cfg.ServiceName, o.cfg.Host)
	exec := fault.WrapExecutor(resilience.ExecutorFor(o.resource), fault.InjectorFor())
	exec = resilobserve.WrapExecutor(exec, "postgresql", o.Observability)
	o.exec = exec
	if err := gormresilience.ApplyCallbacks(o.DB, exec, o.resource); err != nil {
		return err
	}
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor,
// stops any discovery watch behind the client, then closes the underlying
// connection pool.
func (o *DB) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.DB)
	if sqlDB, err := o.DB.DB(); err == nil {
		return sqlDB.Close()
	}
	return nil
}

// liveDialers tracks the discovery-backed resolver behind each client, so the
// wrapper's Close can stop the background watch when the client is torn down.
var liveDialers sync.Map // *gorm.DB -> *discovery.Resolver

// newLiveResolver resolves the registered discovery backend for c and returns a
// Resolver that keeps the service's endpoint set fresh via a background watch. It
// returns (nil, nil) when service-name is unset or mesh mode is enabled (a sidecar
// owns discovery+LB), in which case the caller dials the configured Host directly.
// The caller owns the lifecycle and must release the resolver via stopLiveResolver.
func newLiveResolver(ctx context.Context, c Config) (*discovery.Resolver, error) {
	return discovery.NewResolver(ctx, c.Discovery, c.ServiceName, discovery.WithScheme(c.Scheme))
}

// stopLiveResolver stops the discovery watch behind the given client value. It is
// the Close-half of the discovery lifecycle, symmetric with newLiveResolver; it
// is a no-op for clients that never had a resolver.
func stopLiveResolver(db *gorm.DB) {
	if v, ok := liveDialers.LoadAndDelete(db); ok {
		_ = v.(*discovery.Resolver).Stop()
	}
}
