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

// Package governance is the centralized service-governance authority for the
// process. Where each client starter used to bind its own gs.Dync[resilience.
// Config] and subscribe its own OnChanged handler — eleven near-identical
// copies across redis/gorm/mongo/es/neo4j/bigcache/memcached/gin/http-client/
// gateway — govern collapses that to ONE refreshable [Config] and ONE fan-out.
//
// The governance authority is a process singleton, but callers never hold or
// name a [*Center]: the package exposes a set of free functions (the "facade" in
// global.go — [Enabled], [Driver], [PolicyFor], [Register], [OnReady]) that are
// the sole public surface. [*Center] is an internal implementation detail,
// built and registered by starter-govern; nothing outside this package ever
// obtains one. This mirrors the neutral global seams the package already exposes
// for resilience ([resilience.ExecutorFor]) and fault injection, but is the
// direct surface for callers (like starter-dubbo) that already import governance.
// The single gs.Dync that feeds the authority lives on [Center] itself
// (govern.go); importing cloud/governance is all an app needs to turn ${govern}
// config into live policy. cloud/governance is NOT gs-free — govern.go and
// global.go both import spring/gs. The pure-logic methods (PolicyFor, Refresh,
// Register) stay container-agnostic: [NewCenter] builds a Center usable without
// gs, which is how [Arm] and the package tests drive it.
//
// Governance scope: govern covers every client that goes through the
// resilience Executor seam. dubbo, which has its own URL-param governance
// model, is adapted separately (its timeout/retries are driven from the same
// center via an adapter); dubbo-unique knobs (loadbalance/cluster/serialization)
// stay in a dubbo-specific config section.
package governance

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"go-spring.org/cloud/governance/fault"
	"go-spring.org/cloud/governance/resilience"
	"go-spring.org/spring/gs"
)

// Config is the single source of truth for governance. starter-govern binds it
// once under ${govern} via gs.Dync; every client reads a resolved policy from
// the [Center] instead of carrying its own resilience config. Driver/Enabled
// live at the top level so all resources share one backend selection and one
// on/off switch; per-resource policies live under Rules, matched by the same
// label passed to [Center.PolicyFor] (e.g. "redis:cache", "gorm:mysql:primary",
// "gin:api", "dubbo:com.example.Foo:1.0.0").
type Config struct {
	// Enabled gates the whole center. When false, PolicyFor always returns a
	// zero Policy (transparent pass-through) regardless of Default/Rules,
	// so importing starter-govern without configuring ${govern} is a no-op.
	Enabled bool `value:"${enabled:=false}"`

	// Driver names the resilience backend all resources use ("default" or
	// "sentinel"). Centralizing the driver means one place switches the backend
	// for every client, instead of each starter's own ${...driver}.
	Driver string `value:"${driver:=default}"`

	// Default is the policy applied to every resource that no Rule matches.
	// Most deployments set only this and let every resource share it; per-resource
	// exceptions go under Rules. Bind via govern.default.* (e.g.
	// govern.default.timeout=500ms).
	Default resilience.Config `value:"${default:=}"`

	// Rules are per-resource policy entries. PolicyFor returns the first Rule
	// whose Resources contains the label; when no Rule matches it returns
	// Default. Each Rule's embedded resilience.Config fully replaces Default
	// for the matched resource — not a field-wise merge: a resilience.Policy
	// field of 0 means "disabled", so a partial merge could not distinguish
	// "explicitly set to 0" from "left unset". List more specific Rules first.
	// Bind via indexed properties:
	//
	//	govern.rules[0].resources=redigo:cache
	//	govern.rules[0].timeout=100ms
	//	govern.rules[1].resources=gorm:mysql:orders,gorm:mysql:logs
	//	govern.rules[1].timeout=3s
	//
	// The resource label (with its colons) lives in a value, not a key, so it
	// is dot-safe and needs no escaping in .properties or YAML — unlike a
	// map keyed by label, where the colon would have to appear in the key.
	Rules []Rule `value:"${rules:=}"`

	// Fault is the process-wide fault-injection config (chaos engineering), a
	// sibling concern to resilience governance that rides the SAME ${govern}
	// Dync rather than its own. starter-govern builds one global *fault.Injector
	// from it (in [Center.Init]) and registers it behind the neutral
	// [fault.InjectorFor] seam, so every client/server starter resolves fault
	// injection through that seam instead of each binding its own gs.Dync. Per-
	// resource fault differences live under fault.Config.Rules (matched by the
	// same resource label passed to the executor/Apply seam). A zero Fault
	// (Enabled false) injects nothing. Bind via govern.fault.* (e.g.
	// govern.fault.enabled=true, govern.fault.rate=0.5).
	Fault fault.Config `value:"${fault:=}"`
}

// Rule is one per-resource policy entry. Resources are the resource labels it
// applies to (exact match against any of them); the first matching Rule in
// [Config.Rules] wins, so list specific Rules before broad ones. The embedded
// resilience.Config supplies the policy fields and binds at the same key as the
// Rule (govern.rules[n].timeout, .max-retries, ...), since gs promotes value
// tags through an embedded struct.
type Rule struct {
	// Resources are the resource labels this Rule matches, exact-compare. A
	// resource label is what a starter passes to the executor/fault seam — e.g.
	// "redis:cache", "gorm:mysql:primary", "http:user-svc", "gin::8080". Comma-
	// separated for multiple. Empty matches nothing (use Default instead).
	Resources []string `value:"${resources:=}"`
	resilience.Config
}

// Center is the runtime governance authority. It holds an atomic snapshot of
// the current [Config] and, on [Center.Refresh], notifies every registered
// subscriber whose resolved policy changed — so a single config push fans out
// to all clients through one OnChanged handler, not one per starter bean.
// Safe for concurrent use.
// Center is the runtime governance authority. It holds an atomic snapshot of
// the current [Config] and, on [Center.Refresh], notifies every registered
// subscriber whose resolved policy changed — so a single config push fans out
// to all clients through one OnChanged handler, not one per starter bean.
// Safe for concurrent use.
//
// Center is also the gs-managed singleton (global.go registers the package
// instance [global] as the bean): the ${govern} gs.Dync is a field, and
// [Center.Init] builds the fault injector, arms the OnChanged subscription,
// registers the executor/fault seams, and marks the authority live. [NewCenter]
// is the direct construction path used by tests and [Arm].
type Center struct {
	cfg atomic.Pointer[Config]

	mu   sync.Mutex
	subs map[string][]*subscriber // label -> subscribers

	// The fields below are used only on the gs-managed path. They are left zero
	// by [NewCenter] (tests drive the pure-logic methods directly).

	// Gov is the single source of truth for governance. Field-injected and
	// hot-reloaded: every client's resilience policy AND fault config flow from
	// this one binding (resilience via PolicyFor, fault via injector).
	Gov gs.Dync[Config] `value:"${govern:=}"`

	// injector is the ONE process-wide fault injector, built from Gov's Fault
	// config. It is always built (a disabled injector is a no-op), so fault can
	// be toggled on at runtime via hot-reload; its config is swapped in place
	// from the OnChanged handler.
	injector *fault.Injector

	// labelExecs memoizes the executor built per resource label so the
	// governance subscription (Register) is armed exactly once per label, even
	// if resilience.ExecutorFor hands the label to the provider concurrently on
	// first use.
	labelExecs sync.Map // label -> resilience.Executor
}

// subscriber is one client's interest in the policy for a label. last is the
// most recent policy delivered to cb, so Refresh can skip callbacks whose
// policy is unchanged (no spurious executor rebuilds on an unrelated key
// change) and avoid re-delivering the same value.
type subscriber struct {
	last resilience.Policy
	cb   func(resilience.Policy)
}

// NewCenter snapshots cfg and returns a Center that resolves policies from it.
// The cfg is adopted atomically; callers mutate it only via Refresh. This is the
// direct construction path (tests, [Arm]); the gs-managed path uses the package
// singleton in global.go plus [Center.Init].
func NewCenter(cfg Config) *Center {
	c := &Center{subs: map[string][]*subscriber{}}
	c.cfg.Store(&cfg)
	return c
}

// Init is the gs lifecycle hook (global.go registers it). It snapshots the
// bound ${govern} config into this singleton, builds the process-wide fault
// injector, arms the ONE OnChanged subscription that fans both resilience and
// fault hot-reloads, registers the executor/fault seams, and marks the authority
// live (firing any [OnReady] callbacks). Registering the seams here (rather than
// in a Runner.Run) is safe: both resolve lazily at call time.
func (c *Center) Init() error {
	cfg := c.Gov.Value()
	c.cfg.Store(&cfg)
	c.injector = fault.NewInjector(cfg.Fault)
	c.Gov.OnChanged(func(new, _ Config) {
		c.Refresh(new)
		c.injector.SetConfig(new.Fault)
	})
	resilience.RegisterExecutorProvider(c.executorFor)
	fault.RegisterInjector(c.injector)
	markLive()
	return nil
}

// executorFor is the governance-backed provider registered with
// resilience.RegisterExecutorProvider. For a resource label it builds the
// executor the center resolves (the center's driver + the label's policy) and
// subscribes it to policy changes — so a hot-reload of ${govern} refreshes the
// executor in place. Memoized per label so the subscription is armed once.
func (c *Center) executorFor(label string) resilience.Executor {
	if v, ok := c.labelExecs.Load(label); ok {
		return v.(resilience.Executor)
	}
	exec, err := resilience.NewExecutor(c.Driver(), c.PolicyFor(label))
	if err != nil || exec == nil {
		return nil // resilience.resolve falls back to a no-op executor
	}
	c.Register(label, func(p resilience.Policy) { _ = exec.Refresh(p) })
	actual, _ := c.labelExecs.LoadOrStore(label, exec)
	return actual.(resilience.Executor)
}

// Destroy is a no-op today: the center holds only in-memory subscribers and an
// atomic snapshot, with no background goroutines or closeable resources. It
// exists so the gs lifecycle is symmetric and a future background pump can hook it.
func (c *Center) Destroy() error { return nil }

// Enabled reports whether the center is armed. When false, PolicyFor returns a
// transparent pass-through policy and Register arms clients with a zero Policy.
func (c *Center) Enabled() bool {
	if cfg := c.cfg.Load(); cfg != nil {
		return cfg.Enabled
	}
	return false
}

// Driver returns the configured resilience driver name, defaulting to "default"
// when unset. Clients use it to resolve the Executor backend once, centrally,
// rather than each reading its own ${...driver} knob.
func (c *Center) Driver() string {
	if cfg := c.cfg.Load(); cfg != nil && cfg.Driver != "" {
		return cfg.Driver
	}
	return resilienceDefaultDriver
}

const resilienceDefaultDriver = "default"

// PolicyFor returns the resolved policy for label: the first Rule whose
// Resources contains label, otherwise Default. When the center is disabled it
// returns a zero Policy so an executor armed from it is a transparent
// pass-through. The read is lock-free (atomic pointer load), so the hot path —
// every protected call's caller reads nothing here, only the executor setup
// does — never contends.
func (c *Center) PolicyFor(label string) resilience.Policy {
	cfg := c.cfg.Load()
	if cfg == nil || !cfg.Enabled {
		return resilience.Policy{}
	}
	for _, r := range cfg.Rules {
		if slices.Contains(r.Resources, label) {
			return r.Policy()
		}
	}
	return cfg.Default.Policy()
}

// Register subscribes cb to policy changes for label and arms it immediately
// with the current resolved policy. cb is then invoked whenever [Refresh]
// produces a different policy for label. This is how a client replaces its
// per-bean OnChanged handler: one Register per resource, all driven by the
// center's single Dync.
//
// cb is always called outside the center's lock, so a callback that itself
// calls into the Center cannot self-deadlock; it MUST be safe for concurrent
// invocation, since a Refresh may fire concurrently with this immediate call.
// resilience.Executor.Refresh satisfies that.
//
// The returned policy is the value cb was armed with.
func (c *Center) Register(label string, cb func(resilience.Policy)) resilience.Policy {
	cur := c.PolicyFor(label)
	c.mu.Lock()
	c.subs[label] = append(c.subs[label], &subscriber{last: cur, cb: cb})
	c.mu.Unlock()
	cb(cur)
	return cur
}

// Refresh adopts cfg as the new governance config and notifies every registered
// subscriber whose resolved policy for its label changed. It is the single
// entry point starter-govern calls from its one OnChanged handler — one call
// fans out to all resources, which is the whole point of centralizing.
//
// Notifications are collected under the lock (so last is updated consistently)
// but delivered outside it. A subscriber whose policy is unchanged is not
// notified, keeping a localized change from churning unrelated executors.
func (c *Center) Refresh(cfg Config) {
	c.cfg.Store(&cfg)
	type pending struct {
		cb func(resilience.Policy)
		p  resilience.Policy
	}
	var todo []pending
	c.mu.Lock()
	for label, list := range c.subs {
		next := c.PolicyFor(label)
		for _, s := range list {
			if !policyEqual(s.last, next) {
				s.last = next
				todo = append(todo, pending{cb: s.cb, p: next})
			}
		}
	}
	c.mu.Unlock()
	for _, p := range todo {
		p.cb(p.p)
	}
}

// policyEqual reports whether two center-resolved policies are equivalent for
// the purpose of change detection. resilience.Policy cannot be compared with ==
// because it carries a RetryPredicate func field; but policies produced by the
// center always come from resilience.Config.Policy(), which leaves
// RetryPredicate nil (funcs cannot be bound from value tags), so reflect.DeepEqual
// is exact here. Even if a caller hand-constructs a Config, DeepEqual treats two
// non-nil funcs as equal only when identical, which is the desired semantic.
func policyEqual(a, b resilience.Policy) bool {
	return reflect.DeepEqual(a, b)
}
