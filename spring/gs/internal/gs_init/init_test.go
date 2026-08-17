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

package gs_init

import (
	"testing"

	"go-spring.org/spring/gs/internal/gs_bean"
	"go-spring.org/stdlib/flatten"
	"go-spring.org/stdlib/testing/assert"
)

type initTestBean struct{}

func TestInit(t *testing.T) {

	t.Run("beans returns clones in tests", func(t *testing.T) {
		defer Clear()
		AddBean(gs_bean.NewBean(&initTestBean{}).Name("original"))

		// Under `go test` Beans() hands out clones so that wiring one run
		// never mutates the shared registry entries.
		beans := Beans()
		assert.That(t, len(beans)).Equal(1)
		beans[0].Name("mutated")

		again := Beans()
		assert.That(t, len(again)).Equal(1)
		assert.That(t, again[0].GetName()).Equal("original")
	})

	t.Run("clear empties both registries", func(t *testing.T) {
		AddBean(gs_bean.NewBean(&initTestBean{}))
		AddModule(nil, func(r BeanProvider, p flatten.Storage) error {
			return nil
		}, "init_test.go", 1)
		assert.That(t, len(Beans())).Equal(1)
		assert.That(t, len(Modules())).Equal(1)

		Clear()
		assert.That(t, Beans()).Nil()
		assert.That(t, Modules()).Nil()
	})

	t.Run("add module records file line", func(t *testing.T) {
		defer Clear()
		AddModule(nil, func(r BeanProvider, p flatten.Storage) error {
			return nil
		}, "starter.go", 42)
		modules := Modules()
		assert.That(t, len(modules)).Equal(1)
		assert.That(t, modules[0].FileLine).Equal("starter.go:42")
	})
}
