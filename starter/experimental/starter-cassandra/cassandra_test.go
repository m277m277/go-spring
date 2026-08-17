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

package StarterCassandra

import (
	"testing"

	"github.com/gocql/gocql"
	"go-spring.org/stdlib/testing/assert"
)

// TestParseConsistency covers the config-string mapping, including the
// default and the rejection of unknown values.
func TestParseConsistency(t *testing.T) {
	cases := map[string]gocql.Consistency{
		"":             gocql.LocalQuorum,
		"local-quorum": gocql.LocalQuorum,
		"local-one":    gocql.LocalOne,
		"one":          gocql.One,
		"quorum":       gocql.Quorum,
		"all":          gocql.All,
		"any":          gocql.Any,
		"two":          gocql.Two,
		"three":        gocql.Three,
		"each-quorum":  gocql.EachQuorum,
	}
	for s, want := range cases {
		got, err := parseConsistency(s)
		assert.Error(t, err).Nil()
		assert.That(t, got).Equal(want)
	}
	_, err := parseConsistency("bogus")
	assert.That(t, err != nil).True()
}
