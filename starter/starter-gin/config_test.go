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

package StarterGin

import (
	"testing"

	"go-spring.org/spring/conf"
	"go-spring.org/stdlib/flatten"
	"go-spring.org/stdlib/testing/assert"
)

// PayloadConfig.Limit feeds bufutil.NewLimitedBuffer per request; the expr tag
// must reject bad values at startup instead of letting them surface as a
// per-request panic (negative) or a silently disabled capture (zero).
func TestPayloadConfig_LimitValidatedAtBind(t *testing.T) {
	bind := func(limit any) error {
		p := flatten.NewPropertiesStorage(flatten.MapProperties(map[string]any{
			"limit": limit,
		}))
		var c PayloadConfig
		return conf.Bind(p, &c)
	}

	t.Run("negative limit fails at bind", func(t *testing.T) {
		err := bind(-1)
		assert.Error(t, err).Matches("expr.*\\$ > 0")
	})

	t.Run("zero limit fails at bind", func(t *testing.T) {
		err := bind(0)
		assert.Error(t, err).Matches("expr.*\\$ > 0")
	})

	t.Run("positive limit binds", func(t *testing.T) {
		assert.That(t, bind(1024)).Nil()
	})
}
