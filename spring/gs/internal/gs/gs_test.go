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
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/testing/assert"
)

func TestAs(t *testing.T) {

	t.Run("interface type", func(t *testing.T) {
		s := As[io.Reader]()
		assert.That(t, s.String()).Equal("io.Reader")
	})

	t.Run("non-interface type", func(t *testing.T) {
		assert.Panic(t, func() {
			As[int]()
		}, "T must be interface")
	})
}

func TestBeanSelector(t *testing.T) {

	t.Run("no name", func(t *testing.T) {
		s := BeanIDFor[io.Reader]()
		assert.That(t, s.Name).Equal("")
		assert.That(t, s.Type).Equal(reflect.TypeFor[io.Reader]())
		assert.That(t, fmt.Sprint(s)).Equal("{Type:io.Reader}")
	})

	t.Run("with name", func(t *testing.T) {
		s := BeanIDFor[io.Writer]("writer")
		assert.That(t, s.Name).Equal("writer")
		assert.That(t, s.Type).Equal(reflect.TypeFor[io.Writer]())
		assert.That(t, fmt.Sprint(s)).Equal("{Type:io.Writer,Name:writer}")
	})
}

func TestWrapInjectErr(t *testing.T) {

	cause := errutil.Explain(nil, "boom")

	t.Run("already wrapped error is returned unchanged", func(t *testing.T) {
		wrapped := &InjectionError{Bean: "b1", Err: cause}
		got := WrapInjectErr("b2", wrapped, "ctx %d", 42)
		assert.That(t, got).Same(wrapped) // no duplicate wrapping, no extra context
	})

	t.Run("bean and format", func(t *testing.T) {
		got := WrapInjectErr("b1", cause, "ctx %d", 42)
		var e *InjectionError
		assert.That(t, errors.As(got, &e)).True()
		assert.That(t, e.Bean).Equal("b1")
		assert.String(t, got.Error()).Equal("wire bean b1, err ctx 42: boom")
		assert.String(t, e.Unwrap().Error()).Equal("ctx 42: boom")
		assert.That(t, errors.Unwrap(e.Unwrap())).Same(cause)
	})

	t.Run("format only", func(t *testing.T) {
		got := WrapInjectErr("", cause, "ctx %d", 42)
		var e *InjectionError
		assert.That(t, errors.As(got, &e)).False()
		assert.String(t, got.Error()).Equal("ctx 42: boom")
	})

	t.Run("no args", func(t *testing.T) {
		got := WrapInjectErr("b1", cause)
		var e *InjectionError
		assert.That(t, errors.As(got, &e)).True()
		assert.String(t, got.Error()).Equal("wire bean b1, err boom")
	})

	t.Run("non-string format arg falls back to the original error", func(t *testing.T) {
		got := WrapInjectErr("b1", cause, 42)
		var e *InjectionError
		assert.That(t, errors.As(got, &e)).True()
		assert.That(t, e.Bean).Equal("b1")
		assert.String(t, got.Error()).Equal("wire bean b1, err boom")
	})
}
