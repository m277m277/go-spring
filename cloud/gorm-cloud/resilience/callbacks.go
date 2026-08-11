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

// Package gormresilience is the shared gorm resilience adapter. It replaces
// gorm's standard create/query/update/delete/row/raw callback processors with
// wrappers that run each operation under one [resilience.Executor], so every
// gorm dialect starter (mysql, postgres, clickhouse, sqlserver) shares one
// implementation instead of copy-pasting the ~100-line callback chain each.
//
// It is the gorm seam of resilience: the same backend-neutral Executor that
// other adapters drive through a redis.Hook, an http.RoundTripper or a grpc
// interceptor is here driven through gorm's callback chain. The executor is
// built per-instance by the starter (which knows its Config + driver + the
// optional observe-resilience wrapper); this package only attaches it.
//
// gorm.ErrRecordNotFound is treated as success — "no rows" is a normal outcome,
// not a fault, so it must not trip the breaker (the DB analog of redis.Nil).
package gormresilience

import (
	"context"
	"errors"

	"go-spring.org/cloud/experimental/resilience"
	"gorm.io/gorm"
)

// callbackProcessor is the shape of every gorm callback stage (Create/Query/...
// /Raw): it looks up a registered callback by name and replaces it.
type callbackProcessor interface {
	Get(string) func(*gorm.DB)
	Replace(string, func(*gorm.DB)) error
}

// ApplyCallbacks replaces gorm's six standard processors with wrappers that run
// each operation under exec, scoped to resource. A [gorm.ErrRecordNotFound] from
// the op is treated as success; resilience rejections (ErrRateLimited /
// ErrCircuitOpen / ErrBulkheadFull) surface on tx.Error so the caller sees them.
// It is a no-op for any processor gorm has not registered (Get returns nil).
func ApplyCallbacks(db *gorm.DB, exec resilience.Executor, resource string) error {
	steps := []struct {
		p    callbackProcessor
		name string
	}{
		{db.Callback().Create(), "gorm:create"},
		{db.Callback().Query(), "gorm:query"},
		{db.Callback().Update(), "gorm:update"},
		{db.Callback().Delete(), "gorm:delete"},
		{db.Callback().Row(), "gorm:row"},
		{db.Callback().Raw(), "gorm:raw"},
	}
	for _, s := range steps {
		orig := s.p.Get(s.name)
		if orig == nil {
			continue
		}
		fn := orig
		wrapped := func(tx *gorm.DB) {
			err := runGuard(tx.Statement.Context, exec, resource, func() error {
				fn(tx)
				return tx.Error
			})
			if err != nil && isRejection(err) {
				// Rejected by limiter/breaker/bulkhead before or around the op:
				// surface the rejection on tx.Error so gorm returns it.
				_ = tx.AddError(err)
			}
		}
		if err := s.p.Replace(s.name, wrapped); err != nil {
			return err
		}
	}
	return nil
}

// runGuard executes call under exec, translating rejections but treating
// gorm.ErrRecordNotFound as success. A real op error propagates through the
// executor (feeding retry/breaker); the rejection sentinels are returned as-is
// so ApplyCallbacks' wrapper can put them on tx.Error.
func runGuard(ctx context.Context, exec resilience.Executor, resource string, call func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var callErr error
	execErr := exec.Execute(ctx, resource, func(context.Context) error {
		callErr = call()
		if callErr != nil && !errors.Is(callErr, gorm.ErrRecordNotFound) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or "no rows"
	})
	if execErr != nil {
		if isRejection(execErr) {
			return execErr
		}
		// The executor swallowed/translated a real op error; callErr holds it.
		return callErr
	}
	return callErr
}

// isRejection reports whether err is one of the resilience protection rejects.
func isRejection(err error) bool {
	return errors.Is(err, resilience.ErrRateLimited) ||
		errors.Is(err, resilience.ErrCircuitOpen) ||
		errors.Is(err, resilience.ErrBulkheadFull)
}
