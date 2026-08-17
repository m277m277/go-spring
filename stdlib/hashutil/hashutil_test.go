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

package hashutil

import (
	"hash/fnv"
	"strings"
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestFNV1a64_KnownVectors(t *testing.T) {
	assert.Number(t, FNV1a64("")).Equal(uint64(0xcbf29ce484222325), "offset basis for the empty string")
	assert.Number(t, FNV1a64("a")).Equal(uint64(0xaf63dc4c8601ec8c))
	assert.Number(t, FNV1a64("foobar")).Equal(uint64(0x85944171f73967e8))
}

func TestFNV1a64_MatchesStdlib(t *testing.T) {
	s := strings.Repeat("go-spring ", 100)
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	assert.Number(t, FNV1a64(s)).Equal(h.Sum64())
}
