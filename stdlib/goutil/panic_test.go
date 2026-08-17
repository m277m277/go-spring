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
	"strings"
	"testing"
)

// withTestOnPanic swaps OnPanic for the duration of one test and restores it
// afterwards: it is a package-global slot, so tests must not leak their
// handler into later tests.
func withTestOnPanic(t *testing.T, fn func(context.Context, PanicInfo)) {
	t.Helper()
	old := OnPanic
	OnPanic = fn
	t.Cleanup(func() { OnPanic = old })
}

// TestReportPanicNilIsNoop pins that a nil recovered value reports nothing.
func TestReportPanicNilIsNoop(t *testing.T) {
	fired := false
	withTestOnPanic(t, func(context.Context, PanicInfo) { fired = true })
	ReportPanic(context.Background(), nil) // must not fire the handler
	if fired {
		t.Fatal("nil recovered value must not dispatch")
	}
}

// TestReportPanicReachesHandler proves the shared entry point routes to the
// installed handler with the panic value and a stack that still contains the
// panicking frames.
func TestReportPanicReachesHandler(t *testing.T) {
	var got PanicInfo
	withTestOnPanic(t, func(_ context.Context, info PanicInfo) { got = info })

	ReportPanic(context.Background(), "boom")
	if got.Panic != "boom" {
		t.Fatalf("handler should see the panic value, got %v", got.Panic)
	}
	if !strings.Contains(string(got.Stack), "TestReportPanicReachesHandler") {
		t.Fatalf("stack should contain the reporting frame: %s", got.Stack)
	}
}

// TestSafeRunConvertsPanic proves a panic becomes an error on the normal
// return path (with the panic value and stack in the message) — the property
// scheduler jobs and message handlers rely on.
func TestSafeRunConvertsPanic(t *testing.T) {
	var reported any
	withTestOnPanic(t, func(_ context.Context, info PanicInfo) { reported = info.Panic })

	err := SafeRun(context.Background(), func(context.Context) error {
		panic("boom")
	})
	if err == nil {
		t.Fatal("expected error from panicked SafeRun")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should name the panic value: %v", err)
	}
	if !strings.Contains(err.Error(), "goroutine") {
		t.Fatalf("error should carry the stack: %v", err)
	}
	if reported != "boom" {
		t.Fatalf("OnPanic should have been invoked, got %v", reported)
	}

	// The clean path passes the error through unchanged.
	sentinel := err
	got := SafeRun(context.Background(), func(context.Context) error { return sentinel })
	if got != sentinel {
		t.Fatalf("clean path must pass the error through: %v", got)
	}
}

// TestGoStillRecovers pins the launcher behavior: a panicking goroutine is
// recovered and does not crash the process.
func TestGoStillRecovers(t *testing.T) {
	s := Go(context.Background(), func(context.Context) {
		panic("worker boom")
	}, InheritCancel)
	s.Wait() // returning at all proves the panic was recovered
}
