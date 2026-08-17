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

package gs

import (
	"io"
	"os"
	"strings"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// TestPrintBanner proves a custom banner is printed (with the version line)
// and that an empty banner prints nothing.
func TestPrintBanner(t *testing.T) {
	old := appBanner
	t.Cleanup(func() { appBanner = old })

	capture := func() string {
		r, w, err := os.Pipe()
		assert.That(t, err).Nil()
		saved := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = saved }()

		printBanner()
		_ = w.Close()

		var sb strings.Builder
		_, err = io.Copy(&sb, r)
		assert.That(t, err).Nil()
		return sb.String()
	}

	t.Run("custom banner", func(t *testing.T) {
		Banner("== TEST BANNER ==")
		out := capture()
		assert.That(t, strings.Contains(out, "== TEST BANNER ==")).True()
		assert.That(t, strings.Contains(out, Version)).True()
		assert.That(t, strings.Contains(out, Website)).True()
	})

	t.Run("empty banner", func(t *testing.T) {
		Banner("")
		out := capture()
		assert.That(t, out).Equal("")
	})
}
