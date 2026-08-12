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

// Package gormobservability is the shared GORM instrumentation adapter for the
// go-spring observability kit. It drives one [observe.Observer] for every
// Create/Query/Update/Delete via a gorm.Plugin: a trace span (db.system=<system>),
// a duration/in-flight metric, and an access log (off/brief/detailed) — the same
// three signals every other client starter emits.
//
// It lives in its own module (rather than inside the dep-light observe kit, which
// intentionally does not depend on gorm, or inside the otel-free spring core) so
// the four gorm starters (mysql, postgres, clickhouse, sqlserver) share one
// implementation instead of copy-pasting a ~110-line plugin each. A starter
// installs it with the database's [db.system] semantic-convention value:
//
//	db.Use(gormobserve.NewPlugin("mysql", c.Observability))
package gormobservability

import (
	"sync"

	observe "go-spring.org/observe"
	"gorm.io/gorm"
)

// gormSpans correlates a gorm operation's Before callback (which opens the
// observation for timing) with its After callback (which ends it). gorm hands
// the same *gorm.DB to both, and creates a fresh one per operation, so the
// pointer is a unique key for one in-flight operation.
var gormSpans sync.Map // *gorm.DB -> *observe.Span

// observePlugin is a gorm Plugin that drives the shared observe kit for every
// Create/Query/Update/Delete. The SQL statement is not known until the After
// callback (gorm builds it during the gorm:<op> processor that runs between
// Before and After), so the Before callback opens the span for timing and the
// After callback calls SetArg with the SQL before End — landing the statement in
// the detailed log and the span.
type observePlugin struct {
	obs *observe.Observer
}

func (p *observePlugin) Name() string { return "go-spring:observe" }

func (p *observePlugin) Initialize(db *gorm.DB) error {
	ops := []struct{ kind, raw string }{
		{"create", "gorm:create"},
		{"query", "gorm:query"},
		{"update", "gorm:update"},
		{"delete", "gorm:delete"},
	}
	for _, op := range ops {
		kind, raw := op.kind, op.raw
		before := func(tx *gorm.DB) {
			_, sp := p.obs.Start(tx.Statement.Context, kind, "")
			gormSpans.Store(tx, sp)
		}
		after := func(tx *gorm.DB) {
			v, ok := gormSpans.LoadAndDelete(tx)
			if !ok {
				return
			}
			sp := v.(*observe.Span)
			if tx.Statement != nil {
				sp.SetArg(tx.Statement.SQL.String())
			}
			sp.End(tx.Error)
		}
		// gorm has no generic "register on every processor" API, so register per
		// kind against the gorm:<op> anchor each processor defines by default.
		switch kind {
		case "create":
			if err := db.Callback().Create().Before(raw).Register("go-spring:observe:before_create", before); err != nil {
				return err
			}
			if err := db.Callback().Create().After(raw).Register("go-spring:observe:after_create", after); err != nil {
				return err
			}
		case "query":
			if err := db.Callback().Query().Before(raw).Register("go-spring:observe:before_query", before); err != nil {
				return err
			}
			if err := db.Callback().Query().After(raw).Register("go-spring:observe:after_query", after); err != nil {
				return err
			}
		case "update":
			if err := db.Callback().Update().Before(raw).Register("go-spring:observe:before_update", before); err != nil {
				return err
			}
			if err := db.Callback().Update().After(raw).Register("go-spring:observe:after_update", after); err != nil {
				return err
			}
		case "delete":
			if err := db.Callback().Delete().Before(raw).Register("go-spring:observe:before_delete", before); err != nil {
				return err
			}
			if err := db.Callback().Delete().After(raw).Register("go-spring:observe:after_delete", after); err != nil {
				return err
			}
		}
	}
	return nil
}

// NewPlugin builds a gorm.Plugin that emits trace span + duration/in-flight
// metric + access log for every operation under the given db.system label (e.g.
// "mysql", "postgresql", "clickhouse", "microsoft.sql_server"). cfg controls the
// access log (off/brief/detailed).
func NewPlugin(system string, cfg observe.ObserveConfig) gorm.Plugin {
	return &observePlugin{obs: observe.NewClient(system, cfg)}
}
