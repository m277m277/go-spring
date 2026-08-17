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

package conf

import (
	"reflect"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

type nilConverterTarget struct{}

func TestRegisterNilConverter(t *testing.T) {
	var fn Converter[nilConverterTarget]
	defer delete(converters, reflect.TypeFor[nilConverterTarget]())

	assert.Panic(t, func() {
		RegisterConverter(fn)
	}, "converter for type .* cannot be nil")
}

func TestRegisterNilValidateFunc(t *testing.T) {
	const name = "nilValidateFuncForTest"
	var fn ValidateFunc[int]
	defer delete(validateFuncs, name)

	assert.Panic(t, func() {
		RegisterValidateFunc(name, fn)
	}, "validate function nilValidateFuncForTest cannot be nil")
}

func TestRegisterValidateFuncEmptyName(t *testing.T) {
	assert.Panic(t, func() {
		RegisterValidateFunc("", func(int) (bool, error) { return true, nil })
	}, "validate function name can't be empty")
}

func TestRegisterValidateFuncDuplicateName(t *testing.T) {
	const name = "dupValidateFuncForTest"
	RegisterValidateFunc(name, func(int) (bool, error) { return true, nil })
	defer delete(validateFuncs, name)

	assert.Panic(t, func() {
		RegisterValidateFunc(name, func(int) (bool, error) { return true, nil })
	}, "validate function "+name+" already exists")
}

type dupConverterTarget struct{}

func TestRegisterDuplicateConverter(t *testing.T) {
	RegisterConverter(func(string) (dupConverterTarget, error) { return dupConverterTarget{}, nil })
	defer delete(converters, reflect.TypeFor[dupConverterTarget]())

	assert.Panic(t, func() {
		RegisterConverter(func(string) (dupConverterTarget, error) { return dupConverterTarget{}, nil })
	}, "converter for type .* already exists")
}
