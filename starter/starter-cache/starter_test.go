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

package StarterCache

import (
	"strings"
	"testing"

	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/testing/assert"
)

// noopDriver is a Driver that builds no beans, used to exercise the registry
// without pulling in a real backend.
var noopDriver Driver = func(string) gs.ModuleFunc { return nil }

func TestRegisterDriverPanics(t *testing.T) {
	assert.Panic(t, func() { RegisterDriver("", noopDriver) }, "empty name")
	assert.Panic(t, func() { RegisterDriver("test-nil-driver", nil) }, "nil driver")
}

func TestRegisterAndGetDriver(t *testing.T) {
	RegisterDriver("test-noop-driver", noopDriver)

	_, err := GetDriver("test-noop-driver")
	assert.That(t, err).Nil()

	// Re-registering the same name is a wiring bug; fail loudly at init.
	assert.Panic(t, func() {
		RegisterDriver("test-noop-driver", noopDriver)
	}, "already registered")
}

func TestGetDriverNotFound(t *testing.T) {
	_, err := GetDriver("test-does-not-exist")
	assert.That(t, err).NotNil()
	// The error lists what IS registered so a typo or a missing starter import
	// is obvious.
	assert.That(t, strings.Contains(err.Error(), "no driver registered")).True()
}
