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

package aspect

import (
	"context"
	"fmt"
)

// TxManager abstracts a transactional resource so [Transactional] can bracket an
// operation without depending on any concrete database library. Begin opens a
// transaction and returns a context carrying it (so downstream code discovers the
// transaction through the context) together with an opaque handle; Commit and
// Rollback finalize that handle. A gorm/sql/etc. adapter implements this
// interface in a starter or in application code.
type TxManager interface {
	Begin(ctx context.Context) (context.Context, any, error)
	Commit(tx any) error
	Rollback(tx any) error
}

// Transactional returns an interceptor that provides the declarative-transaction
// equivalent (@Transactional): it begins a transaction, proceeds with the
// tx-carrying context so the business code runs inside it, then commits on
// success or rolls back on error. A panic further down the chain triggers a
// rollback and is re-raised so an outer [Recover] can translate it. A nil
// TxManager makes the interceptor a transparent pass-through.
func Transactional(tm TxManager) Interceptor {
	return InterceptorFunc(func(jp *Joinpoint) (result any, err error) {
		if tm == nil {
			return jp.Proceed(jp.Context)
		}
		ctx, tx, err := tm.Begin(jp.Context)
		if err != nil {
			return nil, fmt.Errorf("aspect: begin transaction for %q: %w", jp.Method, err)
		}
		committed := false
		defer func() {
			if committed {
				return
			}
			// Rollback on error or on a propagating panic. Preserve the original
			// error; surface a rollback failure only when there was none.
			if rbErr := tm.Rollback(tx); rbErr != nil && err == nil {
				err = fmt.Errorf("aspect: rollback transaction for %q: %w", jp.Method, rbErr)
			}
		}()
		result, err = jp.Proceed(ctx)
		if err != nil {
			return nil, err
		}
		if cErr := tm.Commit(tx); cErr != nil {
			return nil, fmt.Errorf("aspect: commit transaction for %q: %w", jp.Method, cErr)
		}
		committed = true
		return result, nil
	})
}
