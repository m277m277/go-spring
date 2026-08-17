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

// Package iterutil provides callback-style loop helpers ([Times], [Ranges],
// [StepRanges]) that run a body function once per iteration. Because the body
// runs inside a callback, a defer inside it fires at the end of each iteration
// rather than when the enclosing function returns.
package iterutil

// Times executes the function 'fn' exactly 'count' times. The loop body runs
// in a callback, so deferred calls inside it execute at the end of each
// iteration rather than when the enclosing function returns.
func Times(count int, fn func(i int)) {
	for i := range count {
		fn(i)
	}
}

// Ranges iterates from 'start' to 'end' (exclusive) and applies 'fn' to each
// index, going forward when start < end and backward otherwise. The loop body
// runs in a callback, so deferred calls inside it execute at the end of each
// iteration rather than when the enclosing function returns.
func Ranges(start, end int, fn func(i int)) {
	if start < end {
		stepRangesForward(start, end, 1, fn)
	} else {
		stepRangesBackward(start, end, -1, fn)
	}
}

// StepRanges iterates from 'start' to 'end' using a step size and applies
// 'fn' to each index; it goes forward when step is positive and start < end,
// and backward when step is negative and start > end. The loop body runs in
// a callback, so deferred calls inside it execute at the end of each
// iteration rather than when the enclosing function returns.
func StepRanges(start, end, step int, fn func(i int)) {
	if step > 0 && start < end {
		stepRangesForward(start, end, step, fn)
	} else if step < 0 && start > end {
		stepRangesBackward(start, end, step, fn)
	}
}

// stepRangesForward helper function for forward step iteration.
func stepRangesForward(start, end, step int, fn func(i int)) {
	for i := start; i < end; i += step {
		fn(i)
	}
}

// stepRangesBackward helper function for backward step iteration.
func stepRangesBackward(start, end, step int, fn func(i int)) {
	for i := start; i > end; i += step {
		fn(i)
	}
}
