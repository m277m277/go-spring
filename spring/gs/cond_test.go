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

package gs_test

import (
	"testing"

	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/testing/assert"
)

// condOf builds a condition that always reports ok, independent of context.
func condOf(ok bool) gs.Condition {
	return gs.OnFunc(func(ctx gs.ConditionContext) (bool, error) {
		return ok, nil
	})
}

// TestConditionCombinators exercises the public Not/Or/And/None combinators.
func TestConditionCombinators(t *testing.T) {
	cases := []struct {
		name string
		cond gs.Condition
		want bool
	}{
		{"Not inverts true", gs.Not(condOf(true)), false},
		{"Not inverts false", gs.Not(condOf(false)), true},
		{"Or all false", gs.Or(condOf(false), condOf(false)), false},
		{"Or one true", gs.Or(condOf(false), condOf(true)), true},
		{"Or all true", gs.Or(condOf(true), condOf(true)), true},
		{"Or empty", gs.Or(), false},
		{"And all true", gs.And(condOf(true), condOf(true)), true},
		{"And one false", gs.And(condOf(true), condOf(false)), false},
		{"And all false", gs.And(condOf(false), condOf(false)), false},
		{"And empty", gs.And(), true},
		{"None all false", gs.None(condOf(false), condOf(false)), true},
		{"None one true", gs.None(condOf(false), condOf(true)), false},
		{"None all true", gs.None(condOf(true), condOf(true)), false},
		{"None empty", gs.None(), true},
		{"nested combinators", gs.And(condOf(true), gs.Or(condOf(false), gs.Not(condOf(false)))), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, err := c.cond.Matches(nil)
			assert.That(t, err).Nil()
			assert.That(t, ok).Equal(c.want)
		})
	}
}
