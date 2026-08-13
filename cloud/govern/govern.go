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

// Package govern is the centralized service-governance authority for the
// process. Where each client starter used to bind its own gs.Dync[resilience.
// Config] and subscribe its own OnChanged handler — eleven near-identical
// copies across redis/gorm/mongo/es/neo4j/bigcache/memcached/gin/http-client/
// gateway — govern collapses that to ONE refreshable [Config] and ONE fan-out.
//
// Client starters inject the [*Center] bean (provided by starter-govern) and
// call [Center.PolicyFor] with their resource label to obtain the resolved
// [resilience.Policy], then [Center.Register] to be notified when it changes.
// The single gs.Dync that feeds the center lives in THIS package (starter.go):
// importing cloud/govern is all an app needs to turn ${govern} config into live
// policy. The pure-logic types below (Config, Center, PolicyFor, Refresh) carry
// no container coupling; starter.go is the one file that imports spring/gs, so
// cloud/govern as a whole is NOT gs-free — gs-wiring lives here rather than in
// a separate starter module.
//
// Governance scope: govern covers every client that goes through the
// resilience Executor seam. dubbo, which has its own URL-param governance
// model, is adapted separately (its timeout/retries are driven from the same
// center via an adapter); dubbo-unique knobs (loadbalance/cluster/serialization)
// stay in a dubbo-specific config section.
package govern

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"go-spring.org/cloud/resilience"
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
type Center struct {
	cfg atomic.Pointer[Config]

	mu   sync.Mutex
	subs map[string][]*subscriber // label -> subscribers
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
// The cfg is adopted atomically; callers mutate it only via Refresh.
func NewCenter(cfg Config) *Center {
	c := &Center{subs: map[string][]*subscriber{}}
	c.cfg.Store(&cfg)
	return c
}

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
