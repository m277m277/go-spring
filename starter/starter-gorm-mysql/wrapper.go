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
	"go-spring.org/cloud/fault"
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
// fault config via gs.Dync so it hot-reloads on config change. newClient
// returns one; gs field-injects Fault + Observability, then calls
// Init (InitMethod) to install the observe plugin and the resilience callbacks.
// Resilience itself is no longer injected here: this DB resolves its executor
// through the neutral [resilience.ExecutorFor] seam, which starter-govern backs
// with the governance center — so this struct has zero coupling to cloud/govern.
type DB struct {
	*gorm.DB
	Fault         gs.Dync[fault.Config] `value:"${fault:=}"`
	Observability observe.ObserveConfig `value:"${observability:=}"`

	cfg      Config // for resourceLabel (addr/service-name)
	exec     resilience.Executor // resolved via resilience.ExecutorFor; no-op when governance is off
	faultInj *fault.Injector
	resource string
}

// Init is the gs InitMethod (runs after gs field-injects Fault +
// Observability). It installs the shared gorm observe plugin, resolves the
// resilience executor through the neutral [resilience.ExecutorFor] seam
// (backed by starter-govern's governance center when configured; a transparent
// no-op otherwise), optionally wraps it with the fault injector, and routes
// every gorm processor through it via [gormresilience.ApplyCallbacks].
func (o *DB) Init() error {
	if o.cfg.ObserveEnabled {
		if err := o.DB.Use(gormobserve.NewPlugin("mysql", o.Observability)); err != nil {
			return err
		}
	}
	o.resource = resilience.ResourceLabel("gorm:mysql", o.cfg.ServiceName, o.cfg.Addr)
	exec := resilience.ExecutorFor(o.resource)
	fc := o.Fault.Value()
	if fc.Enabled {
		o.faultInj = fault.NewInjector(fc)
		exec = fault.WrapExecutor(exec, o.faultInj)
		o.Fault.OnChanged(func(new, _ fault.Config) {
			o.faultInj.SetConfig(new)
		})
	}
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

