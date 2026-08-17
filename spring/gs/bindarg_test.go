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

package gs_test

import (
	"strings"
	"testing"

	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/testing/assert"
)

// gs_arg.Bind stamps its own runtime.Caller(1), which inside the gs.BindArg
// wrapper points at gs/gs.go — the wrapper must re-stamp the user's call site
// so ArgErr diagnostics point at real user code.
func TestBindArgFileLine(t *testing.T) {
	a := gs.BindArg(func() (string, error) { return "", nil })
	s := a.String()
	assert.That(t, strings.Contains(s, "bindarg_test.go")).True(
		"fileline should point at this test file")
	assert.That(t, strings.Contains(s, "gs/gs.go")).False(
		"fileline should not point at the gs wrapper")
}
