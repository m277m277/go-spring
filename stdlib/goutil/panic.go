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

	"go-spring.org/stdlib/errutil"
)

// PanicInfo contains information captured when a panic is recovered.
type PanicInfo struct {
	Panic any
	Stack []byte
}

// OnPanic is a global callback invoked whenever a panic is recovered by this
// package (Go, GoValue, SafeRun) or reported through [ReportPanic] from
// external recover sites such as server interceptors and message-handler
// loops.
//
// By default, it prints the panic value and stack trace to stdout.
// Applications may override this function during initialization to provide
// custom logging, metrics, or alerting behavior.
var OnPanic = func(ctx context.Context, info PanicInfo) {
	fmt.Printf("[PANIC] %v\n%s\n", info.Panic, info.Stack)
}

// ReportPanic reports an already-recovered panic value to [OnPanic],
// capturing the current stack (the panicking frames are still on it when
// called from a deferred recover). The recovered value is reported as-is;
// callers that may hold a nil value should check it themselves.
func ReportPanic(ctx context.Context, recovered any) {
	if OnPanic != nil {
		OnPanic(ctx, PanicInfo{Panic: recovered, Stack: debug.Stack()})
	}
}

// SafeRun runs f synchronously, converting a panic into an error (after
// reporting it through [OnPanic]) so callers that cannot afford a crashing
// goroutine - scheduler jobs, message handlers - get the failure on their
// normal error path instead. Use [Go] for the goroutine form.
func SafeRun(ctx context.Context, f func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			ReportPanic(ctx, r)
			err = errutil.Explain(nil, "panic recovered: %v", r)
		}
	}()
	return f(ctx)
}
