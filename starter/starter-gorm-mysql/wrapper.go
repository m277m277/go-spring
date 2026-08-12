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
	"github.com/go-sql-driver/mysql"
	"go-spring.org/cloud/resilience"
	gormobserve "go-spring.org/starter-gorm/observe"
	"go-spring.org/starter-gorm/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
	"gorm.io/gorm"
)

// DB is the wrapper bean gorm-mysql clients are injected as. It
// embeds *gorm.DB so all gorm methods promote unchanged, and field-injects the
// resilience policy via gs.Dync so it hot-reloads on config change. newClient
// returns one; gs field-injects Resilience + Observability, then calls
// Init (InitMethod) to install the observe plugin and, when armed,
// the resilience callbacks.
type DB struct {
	*gorm.DB
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	cfg      Config // for resourceLabel (addr/service-name)
	exec     resilience.Executor
	resource string
}

// Init is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It installs the shared gorm observe plugin and, when
// resilience is enabled, builds the executor and routes every gorm processor
// through it via [gormresilience.ApplyCallbacks], then subscribes to policy
// changes for hot Refresh.
func (o *DB) Init() error {
	if err := o.DB.Use(gormobserve.NewPlugin("mysql", o.Observability)); err != nil {
		return err
	}
	rc := o.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	exec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "mysql", o.Observability)
	o.exec = exec
	o.resource = resilience.ResourceLabel("gorm:mysql", o.cfg.ServiceName, o.cfg.Addr)
	if err := gormresilience.ApplyCallbacks(o.DB, exec, o.resource); err != nil {
		return err
	}
	// Hot-reload: when the bound resilience config changes, adopt the new policy
	// without a restart. Refresh resets per-resource state (the intended semantic
	// of a threshold change - old failure counts were under the old policy).
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor (if armed),
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

