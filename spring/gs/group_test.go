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
	"sync/atomic"
	"testing"

	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/testing/assert"
)

// groupItemConfig is the per-entry configuration bound from a map value.
type groupItemConfig struct {
	URL string `value:"${url}"`
}

// groupItem is the bean materialized for each map entry.
type groupItem struct {
	Name string
	URL  string
}

// groupDestroyed counts destructor invocations across the app lifecycle.
var groupDestroyed atomic.Int32

func init() {
	gs.Group("${group.items}",
		func(cp *gs.ContextProvider, name string, cfg groupItemConfig) (*groupItem, error) {
			return &groupItem{Name: name, URL: cfg.URL}, nil
		},
		func(c *groupItem) error {
			groupDestroyed.Add(1)
			return nil
		},
	)
}

// TestGroup proves gs.Group materializes one named bean per configuration-map
// entry (the map key becoming the bean name and reaching the constructor's
// name argument) and that the destructor runs for each bean on shutdown.
func TestGroup(t *testing.T) {
	gs.Web(false).Configure(func(app gs.App) {
		app.Property("group.items.a.url", "http://a.example.com")
		app.Property("group.items.b.url", "http://b.example.com")
	}).RunTest(t, func(s *struct {
		Items []*groupItem `autowire:""`
	}) {
		urls := make(map[string]string, len(s.Items))
		for _, it := range s.Items {
			urls[it.Name] = it.URL
		}
		assert.Number(t, len(urls)).Equal(2)
		assert.That(t, urls["a"]).Equal("http://a.example.com")
		assert.That(t, urls["b"]).Equal("http://b.example.com")
	})

	// RunTest has shut the app down before returning, so both destructors
	// must have run by now.
	assert.Number(t, groupDestroyed.Load()).Equal(2)
}
