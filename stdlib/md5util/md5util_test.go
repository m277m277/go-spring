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

package md5util

import (
	"strings"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestMD5_KnownVectors(t *testing.T) {
	assert.String(t, MD5("")).Equal("d41d8cd98f00b204e9800998ecf8427e")
	assert.String(t, MD5("hello")).Equal("5d41402abc4b2a76b9719d911017c592")
	assert.String(t, MD5("The quick brown fox jumps over the lazy dog")).
		Equal("9e107d9d372bb6826bd81d3542a419d6")
}

func TestMD5_OutputFormat(t *testing.T) {
	got := MD5("anything")
	assert.Number(t, len(got)).Equal(32, "MD5 renders as 32 hex chars")
	assert.That(t, got == strings.ToLower(got)).True("MD5 renders lowercase")
}
