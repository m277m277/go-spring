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
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	gormobserve "go-spring.org/starter-gorm/observe"
	"go-spring.org/starter-gorm/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
	"gorm.io/gorm"
)

// DB is the wrapper bean gorm-clickhouse clients are injected as. It
// embeds *gorm.DB so all gorm methods promote unchanged, and field-injects the
// resilience policy via gs.Dync so it hot-reloads on config change. newClient
// returns one; gs field-injects Resilience + Observability, then calls
// Init (InitMethod) to install the observe plugin and, when armed,
// the resilience callbacks.
type DB struct {
	*gorm.DB
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Fault         gs.Dync[fault.Config]      `value:"${fault:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	cfg      Config // for resourceLabel (addr/service-name)
	exec     resilience.Executor
	faultInj *fault.Injector
	resource string
}

// Init is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It installs the shared gorm observe plugin and, when
// resilience is enabled, builds the executor and routes every gorm processor
// through it via [gormresilience.ApplyCallbacks], then subscribes to policy
// changes for hot Refresh.
func (o *DB) Init() error {
	if err := o.DB.Use(gormobserve.NewPlugin("clickhouse", o.Observability)); err != nil {
		return err
	}
	rc := o.Resilience.Value()
	fc := o.Fault.Value()
	if !rc.Enabled && !fc.Enabled {
		return nil
	}
	rawExec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec := rawExec
	if fc.Enabled {
		o.faultInj = fault.NewInjector(fc)
		exec = fault.WrapExecutor(rawExec, o.faultInj)
	}
	exec = resilobserve.WrapExecutor(exec, "clickhouse", o.Observability)
	o.exec = exec
	o.resource = resilience.ResourceLabel("gorm:clickhouse", o.cfg.ServiceName, o.cfg.Addr)
	if err := gormresilience.ApplyCallbacks(o.DB, exec, o.resource); err != nil {
		return err
	}
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		_ = exec.Refresh(new.Policy())
	})
	if o.faultInj != nil {
		o.Fault.OnChanged(func(new, _ fault.Config) {
			o.faultInj.SetConfig(new)
		})
	}
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor (if armed),
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

