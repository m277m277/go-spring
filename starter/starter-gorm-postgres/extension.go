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

package StarterGormPostgres

import (
	"sync"

	"gorm.io/gorm"
)

// DBCustomizer tweaks a freshly-opened *gorm.DB for an instance before the
// starter wraps and returns it. It is the post-open extension seam for users
// who need to adjust what gorm.Open does not expose through config — the
// underlying *sql.DB pool knobs (ConnMaxIdleTime/MaxOpenConns via db.DB()),
// a custom gorm.Logger, an extra Plugin, prepared-statement caching, etc. —
// without copying the starter. Register via [UseDBCustomizer]; called in
// registration order after gorm.Open + pool apply, before the bean is returned.
type DBCustomizer func(c Config, db *gorm.DB) error

var (
	customizerMu sync.RWMutex
	customizers  []DBCustomizer
)

// UseDBCustomizer appends a customizer run for every instance at creation,
// after the connection pool is applied and before the bean is returned. Call
// from an init function (or otherwise before the container wires). Multiple
// calls compose in registration order; the first non-nil error stops the chain
// and fails the instance.
func UseDBCustomizer(f DBCustomizer) {
	if f == nil {
		return
	}
	customizerMu.Lock()
	defer customizerMu.Unlock()
	customizers = append(customizers, f)
}

// applyDBCustomizers runs the registered customizers against db. No-op when
// none are registered.
func applyDBCustomizers(c Config, db *gorm.DB) error {
	customizerMu.RLock()
	defer customizerMu.RUnlock()
	for _, f := range customizers {
		if err := f(c, db); err != nil {
			return err
		}
	}
	return nil
}
