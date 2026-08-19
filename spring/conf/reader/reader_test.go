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

package reader

import (
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestRegisterNilReader(t *testing.T) {
	const ext = ".nil-reader-test"
	var r Reader
	defer delete(readers, ext)

	assert.Panic(t, func() {
		Register(r, ext)
	}, "reader cannot be nil")
}

func TestRegisterEmptyExt(t *testing.T) {
	dummy := func(b []byte) (map[string]any, error) { return nil, nil }

	assert.Panic(t, func() {
		Register(dummy, "")
	}, "file extension cannot be empty")
}

func TestRegisterDuplicateExt(t *testing.T) {
	const ext = ".dup-reader-test"
	dummy := func(b []byte) (map[string]any, error) { return nil, nil }
	Register(dummy, ext)
	defer delete(readers, ext)

	assert.Panic(t, func() {
		Register(dummy, ext)
	}, "file extension "+ext+" has been registered")
}

func TestHas(t *testing.T) {
	// a format name and its extension are the same registry key
	assert.That(t, Has("json")).True()
	assert.That(t, Has(".json")).True()
	assert.That(t, Has("properties")).True()
	assert.That(t, Has(".properties")).True()
	assert.That(t, Has(".props")).True()
	assert.That(t, Has("yaml")).True()
	assert.That(t, Has(".yml")).True()
	assert.That(t, Has("toml")).True()
	assert.That(t, Has(".tml")).True()

	assert.That(t, Has("ini")).False()
	assert.That(t, Has(".ini")).False()
	assert.That(t, Has("")).False()
}

func TestRead(t *testing.T) {
	t.Run("format name and extension are the same key", func(t *testing.T) {
		want := map[string]any{"a": float64(1)}

		m, err := Read("json", []byte(`{"a":1}`))
		assert.That(t, err).Nil()
		assert.That(t, m).Equal(want)

		m, err = Read(".json", []byte(`{"a":1}`))
		assert.That(t, err).Nil()
		assert.That(t, m).Equal(want)
	})

	t.Run("unsupported format", func(t *testing.T) {
		_, err := Read("ini", []byte(""))
		assert.Error(t, err).Matches(`unsupported config format "ini"`)
	})
}

func TestReadFile(t *testing.T) {
	t.Run("parses by file extension", func(t *testing.T) {
		m, err := ReadFile("../testdata/config/app.properties")
		assert.That(t, err).Nil()
		assert.That(t, m).Equal(map[string]any{
			"properties.list[0]":          "1",
			"properties.list[1]":          "2",
			"properties.obj.list[0].age":  "4",
			"properties.obj.list[0].name": "tom",
			"properties.obj.list[1].age":  "2",
			"properties.obj.list[1].name": "jerry",
		})
	})

	t.Run("unsupported extension", func(t *testing.T) {
		_, err := ReadFile("../testdata/config/app.unknown")
		assert.Error(t, err).Matches(`unsupported config format "\.unknown"`)
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := ReadFile("./missing.properties")
		assert.Error(t, err).Matches("no such file or directory")
	})
}
