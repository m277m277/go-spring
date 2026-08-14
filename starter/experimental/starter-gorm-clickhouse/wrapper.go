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
	"go-spring.org/cloud/governance/fault"
	"go-spring.org/cloud/governance/resilience"
	observe "go-spring.org/cloud/observe"
	"go-spring.org/cloud/observe/resilience"
	gormobserve "go-spring.org/starter-gorm/observe"
	"go-spring.org/starter-gorm/resilience"
	"gorm.io/gorm"
)

// DB is the wrapper bean gorm-clickhouse clients are injected as. It
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
		if err := o.DB.Use(gormobserve.NewPlugin("clickhouse", o.Observability)); err != nil {
			return err
		}
	}
	o.resource = resilience.ResourceLabel("gorm:clickhouse", o.cfg.ServiceName, o.cfg.Addr)
	exec := fault.WrapExecutor(resilience.ExecutorFor(o.resource), fault.InjectorFor())
	exec = resilobserve.WrapExecutor(exec, "clickhouse", o.Observability)
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
