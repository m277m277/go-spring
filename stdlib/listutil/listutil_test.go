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

package listutil

import (
	"errors"
	"strings"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestSliceOf(t *testing.T) {
	assert.That(t, SliceOf(1, 2, 3)).Equal([]int{1, 2, 3})
	assert.Number(t, len(SliceOf[string]())).Zero()

	s := SliceOf("a")
	s = append(s, "b") // the returned slice belongs to the caller
	assert.That(t, s).Equal([]string{"a", "b"})
}

func TestListOf(t *testing.T) {
	l := ListOf(1, 2, 3)
	assert.Number(t, l.Len()).Equal(3)

	var values []int
	for e := l.Front(); e != nil; e = e.Next() {
		values = append(values, e.Value.(int))
	}
	assert.That(t, values).Equal([]int{1, 2, 3})

	empty := ListOf[int]()
	assert.Number(t, empty.Len()).Zero()
}

func TestAllOfList(t *testing.T) {
	t.Run("normal list", func(t *testing.T) {
		assert.That(t, AllOfList[int](ListOf(1, 2, 3))).Equal([]int{1, 2, 3})
	})

	t.Run("nil list", func(t *testing.T) {
		assert.That(t, AllOfList[int](nil)).Nil()
	})

	t.Run("empty list", func(t *testing.T) {
		assert.That(t, AllOfList[int](ListOf[int]())).Nil()
	})

	t.Run("mismatched element type panics", func(t *testing.T) {
		assert.Panic(t, func() {
			_ = AllOfList[string](ListOf(1, 2, 3))
		}, "interface conversion")
	})
}

// errWriter records every string it is asked to write and fails with err on
// the first one, so tests can verify that WriteStrings stops early.
type errWriter struct {
	err    error
	writes []string
}

func (w *errWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	return 0, w.err
}

func TestWriteStrings(t *testing.T) {
	t.Run("writes all strings in order", func(t *testing.T) {
		var sb strings.Builder
		err := WriteStrings(&sb, "hello", ",", " ", "world")
		assert.That(t, err).Nil()
		assert.String(t, sb.String()).Equal("hello, world")
	})

	t.Run("no strings", func(t *testing.T) {
		var sb strings.Builder
		err := WriteStrings(&sb)
		assert.That(t, err).Nil()
		assert.String(t, sb.String()).Equal("")
	})

	t.Run("stops at the first error", func(t *testing.T) {
		w := &errWriter{err: errors.New("boom")}
		err := WriteStrings(w, "a", "b", "c")
		assert.That(t, err).NotNil()
		assert.Error(t, err).Matches("boom")
		// "b" and "c" must never reach the writer.
		assert.That(t, w.writes).Equal([]string{"a"})
	})
}
