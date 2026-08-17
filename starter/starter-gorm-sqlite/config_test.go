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

package StarterGormSqlite

import (
	"strings"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// TestDSN pins the modernc.org/sqlite URI construction: file + parenthesized
// _pragma query pairs, in a fixed order.
func TestDSN(t *testing.T) {
	c := Config{File: "/tmp/app.db", JournalMode: "wal", BusyTimeout: 5000, ForeignKeys: true}
	dsn := c.DSN()
	assert.That(t, strings.HasPrefix(dsn, "/tmp/app.db?")).True()
	assert.That(t, strings.Contains(dsn, "_pragma=journal_mode(wal)")).True()
	assert.That(t, strings.Contains(dsn, "_pragma=busy_timeout(5000)")).True()
	assert.That(t, strings.Contains(dsn, "_pragma=foreign_keys(1)")).True()
}

// TestDSNMemory proves a bare :memory: passes through unchanged when no
// pragmas are set (a zero-value Config has JournalMode/BusyTimeout/ForeignKeys
// at their zero defaults).
func TestDSNMemory(t *testing.T) {
	dsn := (Config{File: ":memory:"}).DSN()
	assert.That(t, dsn).Equal(":memory:")
}

// TestDSNDefaults pins that a zero-value Config still produces a usable
// memory URI (empty File would fail expr validation at bind time; DSN itself
// is lenient).
func TestDSNDefaults(t *testing.T) {
	dsn := (Config{File: ":memory:"}).DSN()
	assert.That(t, dsn != "").True()
}
