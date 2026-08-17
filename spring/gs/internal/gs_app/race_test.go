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

package gs_app

import (
	"sync"
	"testing"

	"go-spring.org/spring/gs/internal/gs_dync"
	"go-spring.org/stdlib/testing/assert"
)

// dyncConfig registers a gs.Dync field so the started app keeps its
// configuration (Start only drops it when DynamicObjectsCount()==0).
type dyncConfig struct {
	Addr gs_dync.Value[string] `value:"${addr:=127.0.0.1:5050}"`
}

// RefreshProperties is documented as callable from any goroutine. The app's
// configuration pointer is stored in an atomic.Pointer (it is dropped by
// Store(nil) inside Start when no dynamic values exist), so concurrent
// refreshes must be race-free — run with -race to make that a hard gate.
func TestRefreshProperties_Concurrent(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	app := NewApp()
	app.c.Provide(&dyncConfig{})
	err := app.Start()
	assert.That(t, err).Nil()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = app.RefreshProperties()
		}()
	}
	wg.Wait()
}
