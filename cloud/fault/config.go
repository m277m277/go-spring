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

package fault

import "time"

// Config binds the fault-injection knobs from go-spring ${...} value tags and
// mirrors the binding style of [resilience.Config]: a single shared struct each
// client starter embeds under its own key prefix (redigo uses ${fault.*}). A
// zero Config (Enabled false) injects nothing and [WrapExecutor] is not
// installed by the starter.
//
// The MVP is one global rule: Latency applies to every call, Error applies at
// the configured Rate. For redigo (a single resource) global == per-resource;
// per-resource Rules are a near-term extension once gs value binding for lists
// is confirmed.
type Config struct {
	// Enabled turns fault injection on. When false no faults are injected even
	// if Rate/Latency/Error are set.
	Enabled bool `value:"${enabled:=false}"`

	// Rate is the per-call probability (0..1) of injecting the configured Error.
	// 0 means never inject; 1 means every call fails. Latency, when set, is
	// applied to every call regardless of Rate — use it to model a uniformly
	// slow downstream without forcing errors.
	Rate float64 `value:"${rate:=0}"`

	// Latency is injected before each call (and before the error decision). Use
	// it to exercise slow-call and per-attempt timeout paths. 0 disables.
	Latency time.Duration `value:"${latency:=0}"`

	// Error selects the injected failure kind, applied at Rate:
	//   "" / "generic" — a retryable injected error ([ErrInjected])
	//   "timeout"      — wraps context.DeadlineExceeded
	//   "reset"        — wraps syscall.ECONNRESET
	// Empty (the default) plus Rate 0 means no errors are injected.
	Error string `value:"${error:=}"`
}
