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
	observe "go-spring.org/observe"
	gormobserve "go-spring.org/gorm-cloud/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/gorm-cloud/resilience"
	"go-spring.org/cloud/experimental/resilience"
	"go-spring.org/spring/gs"
	"gorm.io/gorm"
)

// ObservedGormDB is the wrapper bean gorm-clickhouse clients are injected as. It
// embeds *gorm.DB so all gorm methods promote unchanged, and field-injects the
// resilience policy via gs.Dync so it hot-reloads on config change. newClient
// returns one; gs field-injects Resilience + Observability, then calls
// ApplyResilience (InitMethod) to install the observe plugin and, when armed,
// the resilience callbacks.
type ObservedGormDB struct {
	*gorm.DB
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Observability observe.LogConfig          `value:"${observability:=}"`

	cfg      Config // for resourceLabel (addr/service-name)
	exec     resilience.Executor
	resource string
}

// ApplyResilience is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It installs the shared gorm observe plugin and, when
// resilience is enabled, builds the executor and routes every gorm processor
// through it via [gormresilience.ApplyCallbacks], then subscribes to policy
// changes for hot Refresh.
func (o *ObservedGormDB) ApplyResilience() error {
	if err := o.DB.Use(gormobserve.NewPlugin("clickhouse", o.Observability)); err != nil {
		return err
	}
	rc := o.Resilience.Value()
	if !rc.Enabled {
		return nil
	}
	drv, err := resilience.MustGetDriver(rc.Driver)
	if err != nil {
		return err
	}
	exec, err := drv.NewExecutor(rc.Policy())
	if err != nil {
		return err
	}
	exec = resilobserve.WrapExecutor(exec, "clickhouse", o.Observability)
	o.exec = exec
	o.resource = resourceLabel(o.cfg)
	if err := gormresilience.ApplyCallbacks(o.DB, exec, o.resource); err != nil {
		return err
	}
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		if r, ok := exec.(resilience.RefreshableExecutor); ok {
			_ = r.Refresh(new.Policy())
		}
	})
	return nil
}

// Close is the gs destroy method: closes the resilience executor (if armed),
// stops any discovery watch behind the client, then closes the underlying
// connection pool.
func (o *ObservedGormDB) Close() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	stopLiveResolver(o.DB)
	if sqlDB, err := o.DB.DB(); err == nil {
		return sqlDB.Close()
	}
	return nil
}

// resourceLabel derives a stable resilience resource key for a client, so
// limiter and breaker state is scoped per DB instance rather than per statement.
func resourceLabel(c Config) string {
	return resilience.ResourceLabel("gorm:clickhouse", c.ServiceName, c.Addr)
}
