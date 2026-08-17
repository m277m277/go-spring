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

package patchutil

import (
	"reflect"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

type secretHolder struct {
	secret string
}

func fieldOf(obj any, name string) reflect.Value {
	return reflect.ValueOf(obj).Elem().FieldByName(name)
}

func TestPatchValue_EnablesSetOnUnexportedField(t *testing.T) {
	obj := &secretHolder{secret: "old"}
	field := PatchValue(fieldOf(obj, "secret"))
	field.SetString("new")
	assert.String(t, obj.secret).Equal("new")
}

func TestUnpatchedValue_CannotSet(t *testing.T) {
	obj := &secretHolder{secret: "old"}
	assert.Panic(t, func() {
		fieldOf(obj, "secret").SetString("new")
	}, "using value obtained using unexported field")
	assert.String(t, obj.secret).Equal("old", "the failed Set left the field untouched")
}
