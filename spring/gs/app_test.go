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

func init() {
	gs.Provide(func(ctx *gs.ContextProvider) *GlobalService {
		return &GlobalService{}
	})
}

type GlobalService struct {
	Name string `value:"${name:=global}"`
}

type App1Service struct {
	Name string         `value:"${name:=app1}"`
	Svr  *GlobalService `autowire:""`
}

func TestApp1(t *testing.T) {
	gs.Web(false).RunTest(t, func(s *App1Service) {
		assert.That(t, s.Name).Equal("app1")
		assert.That(t, s.Svr).NotNil()
		assert.That(t, s.Svr.Name).Equal("global")
	})
}

func TestApp2(t *testing.T) {
	gs.Web(false).Configure(func(g gs.App) {
		g.Property("name", "myapp2")
	}).RunTest(t, func(s *struct {
		Name string         `value:"${name:=app2}"`
		Svr  *GlobalService `autowire:""`
		App1 *App1Service   `autowire:"?"`
	}) {
		assert.That(t, s.Name).Equal("myapp2")
		assert.That(t, s.Svr).NotNil()
		assert.That(t, s.Svr.Name).Equal("myapp2")
		assert.That(t, s.App1).Nil() // no App1Service bean in this app
	})
}

// TestWeb proves Web(false) keeps the built-in HTTP server module from
// registering its beans, and that the server is provided by default
// (on an ephemeral port) when not disabled.
func TestWeb(t *testing.T) {

	t.Run("server disabled", func(t *testing.T) {
		gs.Web(false).RunTest(t, func(s *struct {
			Svr *gs.SimpleHttpServer `autowire:"?"`
		}) {
			assert.That(t, s.Svr).Nil()
		})
	})

	t.Run("server enabled", func(t *testing.T) {
		gs.Configure(func(app gs.App) {
			app.Property("spring.http.server.addr", ":0")
		}).RunTest(t, func(s *struct {
			Svr *gs.SimpleHttpServer `autowire:""`
		}) {
			assert.That(t, s.Svr).NotNil()
		})
	})
}
