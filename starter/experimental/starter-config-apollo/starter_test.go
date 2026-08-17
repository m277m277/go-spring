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

package StarterConfigApollo

import (
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

// TestParseSource pins the source grammar and defaults: cluster "default",
// format inferred from the namespace extension.
func TestParseSource(t *testing.T) {
	cs, err := parseSource("127.0.0.1:8080/application.properties?appId=demo")
	assert.Error(t, err).Nil()
	assert.That(t, cs.server).Equal("127.0.0.1:8080")
	assert.That(t, cs.namespace).Equal("application.properties")
	assert.That(t, cs.appID).Equal("demo")
	assert.That(t, cs.cluster).Equal("default")
	assert.That(t, cs.format).Equal("properties")
}

// TestParseSourceMissingAppID pins the required appId.
func TestParseSourceMissingAppID(t *testing.T) {
	_, err := parseSource("127.0.0.1:8080/application")
	assert.That(t, err != nil).True()
}

// TestParseSourceFormatOverride pins an explicit format query over the
// namespace extension.
func TestParseSourceFormatOverride(t *testing.T) {
	cs, err := parseSource("h:1/ns?appId=demo&format=yaml&cluster=prod")
	assert.Error(t, err).Nil()
	assert.That(t, cs.format).Equal("yaml")
	assert.That(t, cs.cluster).Equal("prod")
}
