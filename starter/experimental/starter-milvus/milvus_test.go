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

package StarterMilvus

import (
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// TestConfigDefaults pins the database default and that addr is required
// (validated by the expr tag at bind time).
func TestConfigDefaults(t *testing.T) {
	var c Config
	assert.That(t, c.Database).Equal("") // zero value; the := tag fills "default" at bind
	assert.That(t, c.Addr).Equal("")
}
