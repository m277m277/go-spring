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

// This file holds the governance singleton, its public facade, and its gs
// binding. The governance authority is a process singleton, but callers never
// hold or name a [*Center]: they call the package-level functions (Enabled,
// PolicyFor, Register, OnReady). [*Center] is an internal implementation detail.
// This mirrors the neutral global seams the package already exposes for
// resilience ([resilience.ExecutorFor]) and fault injection, but is the direct
// surface for callers (like starter-dubbo) that already import governance.

package governance

import (
	"go-spring.org/cloud/governance/resilience"
	"go-spring.org/spring/gs"
)

// global is the process-wide governance authority. It is initialized to a
// disabled (zero-config) Center at package load, so it is never nil. In
// production gs adopts this very instance as its bean ([init] below), field-
// injects the ${govern} gs.Dync onto it, and runs [Center.Init] which mutates it
// in place with the real config. Tests reassign it via [Arm]/[Reset]. Because it
// is armed once during gs wiring (or a test) and only read after, the plain
// pointer needs no atomic or mutex — gs establishes the happens-before edge
// between wiring and server start.
var global = NewCenter(Config{})

// live marks whether global has been armed with real config — set by
// [Center.Init] (production) or [Arm] (tests), cleared by [Reset]. [OnReady]
// queues callbacks until live, then fires them. It cannot key off global itself
// being non-nil, because global is never nil (it starts as a disabled Center).
var (
	live     bool
	readyCbs []func()
)

// markLive flags the authority armed and fires every callback queued via
// [OnReady]. Wiring-time only (single goroutine), so the queue needs no lock.
func markLive() {
	live = true
	cbs := readyCbs
	readyCbs = nil
	for _, cb := range cbs {
		cb()
	}
}

func init() {
	// Register the singleton itself as the gs bean: no separate holder. gs
	// field-injects the ${govern} gs.Dync onto global, then runs [Center.Init]
	// which arms it. Exported as a gs.Rooter so gs collects and instantiates it
	// even though no client injects it — without a collected-type export gs would
	// not instantiate an unreachable bean, so none of the registrations fire.
	gs.Provide(global).
		Init((*Center).Init).Destroy((*Center).Destroy).
		Export(gs.As[gs.Rooter]()).Caller(1)
}

// Enabled reports whether governance is armed. Before the authority is live it
// returns false (the singleton starts as a disabled Center) — the same as a
// registered-but-disabled authority.
func Enabled() bool { return global.Enabled() }

// Driver returns the configured resilience driver name, defaulting to "default"
// when unset (including before the authority is live).
func Driver() string { return global.Driver() }

// PolicyFor returns the resolved policy for label. Before the authority is live
// it returns a zero (pass-through) policy. It is the function callers use instead
// of holding a [*Center]: pass your resource label, get the policy.
func PolicyFor(label string) resilience.Policy { return global.PolicyFor(label) }

// Register subscribes cb to policy changes for label, arms it immediately with
// the current resolved policy, and returns that policy. This is how a caller
// subscribes to governance hot-reload without ever touching a [*Center].
func Register(label string, cb func(resilience.Policy)) resilience.Policy {
	return global.Register(label, cb)
}

// OnReady registers cb to fire exactly once when the authority goes live. If it
// is already live, cb fires immediately. gs wires Rooters before Runners, so a
// push-based caller (starter-dubbo, a Rooter) may initialize before this
// package's Rooter has run [Center.Init]; OnReady guarantees the caller re-runs
// its work once governance is live, without depending on bean init order.
func OnReady(cb func()) {
	if live {
		cb()
		return
	}
	readyCbs = append(readyCbs, cb)
}

// Arm installs an authority built from cfg onto the singleton and returns a
// reset function that restores the disabled default. It is the non-gs wiring
// path: tests (and any standalone use without gs) drive governance through it.
func Arm(cfg Config) (reset func()) {
	global = NewCenter(cfg)
	markLive()
	return Reset
}

// Reset restores the singleton to a disabled (zero-config) authority and clears
// the live flag. It is the cleanup counterpart to [Arm].
func Reset() {
	global = NewCenter(Config{})
	live = false
}
