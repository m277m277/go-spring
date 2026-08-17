/*
 * Copyright 2024 The Go-Spring Authors.
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

package goutil

import (
	"context"
	"fmt"
	"runtime/debug"
)

// This file widens the single-handler [OnPanic] mechanism beyond the Go /
// GoValue launchers: [ReportPanic] is the reporting entry for recover sites
// that cannot route through those launchers (server recovery interceptors,
// message-handler loops, job runners), and [SafeRun] is the synchronous
// panic-to-error form. One handler slot by design — see OnPanic.

// ReportPanic reports an already-recovered panic value to [OnPanic],
// capturing the current stack (the panicking frames are still on it when
// called from a deferred recover). It is a no-op for a nil recovered value.
func ReportPanic(ctx context.Context, recovered any) {
	if recovered == nil {
		return
	}
	if OnPanic != nil {
		OnPanic(ctx, PanicInfo{Panic: recovered, Stack: debug.Stack()})
	}
}

// SafeRun runs f synchronously, converting a panic into an error (after
// reporting it through [OnPanic]) so callers that cannot afford a crashing
// goroutine — scheduler jobs, message handlers — get the failure on their
// normal error path instead. Use [Go] for the goroutine form.
func SafeRun(ctx context.Context, f func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			ReportPanic(ctx, r)
			err = fmt.Errorf("panic recovered: %v\n%s", r, stack)
		}
	}()
	return f(ctx)
}
