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

package govern

import (
	"testing"
	"time"

	"go-spring.org/cloud/resilience"
)

// dur is a shorthand for declaring timeouts in test policies.
func dur(d int) time.Duration { return time.Duration(d) * time.Millisecond }

// enabledTimeout returns a Config whose Default policy sets a per-attempt
// timeout, the knob the center exists to centralize.
func enabledTimeout(d int) Config {
	return Config{
		Enabled: true,
		Default: resilience.Config{Enabled: true, AttemptTimeout: dur(d)},
	}
}

func TestPolicyFor_Default(t *testing.T) {
	c := NewCenter(enabledTimeout(100))
	if p := c.PolicyFor("redis:cache"); p.Timeout != dur(100) {
		t.Fatalf("PolicyFor default: want timeout 100ms, got %v", p.Timeout)
	}
	// Unknown label also falls back to Default.
	if p := c.PolicyFor("anything:else"); p.Timeout != dur(100) {
		t.Fatalf("PolicyFor fallback: want timeout 100ms, got %v", p.Timeout)
	}
}

func TestPolicyFor_RuleReplacesDefault(t *testing.T) {
	c := NewCenter(Config{
		Enabled: true,
		Default: resilience.Config{Enabled: true, AttemptTimeout: dur(100), MaxRetries: 1},
		Rules: []Rule{{
			Resources: []string{"redis:cache"},
			Config:    resilience.Config{Enabled: true, AttemptTimeout: dur(50)}, // no MaxRetries
		}},
	})
	p := c.PolicyFor("redis:cache")
	if p.Timeout != dur(50) {
		t.Fatalf("rule timeout: want 50ms, got %v", p.Timeout)
	}
	// A Rule fully replaces Default, so MaxRetries from Default does NOT carry
	// over — that is the documented "complete, self-contained policy" semantic.
	if p.MaxRetries != 0 {
		t.Fatalf("rule must replace not merge: want MaxRetries 0, got %d", p.MaxRetries)
	}
	// A label no Rule matches still gets Default.
	if p := c.PolicyFor("redis:other"); p.Timeout != dur(100) {
		t.Fatalf("unmatched label: want default 100ms, got %v", p.Timeout)
	}
}

func TestPolicyFor_FirstMatchingRuleWins(t *testing.T) {
	// When two Rules match the same label, the earlier one wins — so list
	// specific Rules before broad ones.
	c := NewCenter(Config{
		Enabled: true,
		Rules: []Rule{
			{Resources: []string{"redis:cache"}, Config: resilience.Config{Enabled: true, AttemptTimeout: dur(10)}},
			{Resources: []string{"redis:cache"}, Config: resilience.Config{Enabled: true, AttemptTimeout: dur(20)}},
		},
	})
	if p := c.PolicyFor("redis:cache"); p.Timeout != dur(10) {
		t.Fatalf("first matching rule should win: want 10ms, got %v", p.Timeout)
	}
	// A Rule with empty Resources matches nothing (use Default instead).
	c2 := NewCenter(Config{
		Enabled: true,
		Default: resilience.Config{Enabled: true, AttemptTimeout: dur(100)},
		Rules:   []Rule{{Config: resilience.Config{Enabled: true, AttemptTimeout: dur(50)}}},
	})
	if p := c2.PolicyFor("redis:cache"); p.Timeout != dur(100) {
		t.Fatalf("empty-Resources rule must not match: want default 100ms, got %v", p.Timeout)
	}
}

func TestPolicyFor_DisabledIsPassThrough(t *testing.T) {
	// Enabled=false makes the center a no-op even when Default is set.
	c := NewCenter(Config{Enabled: false, Default: resilience.Config{Enabled: true, AttemptTimeout: dur(100)}})
	if p := c.PolicyFor("redis:cache"); !p.IsZero() {
		t.Fatalf("disabled center must yield zero policy, got %+v", p)
	}
	if c.Enabled() {
		t.Fatal("Enabled() should be false")
	}
}

func TestRegister_ArmsImmediately(t *testing.T) {
	c := NewCenter(enabledTimeout(100))
	var got resilience.Policy
	c.Register("redis:cache", func(p resilience.Policy) { got = p })
	if got.Timeout != dur(100) {
		t.Fatalf("Register should arm cb with current policy, got timeout %v", got.Timeout)
	}
}

func TestRefresh_NotifiesOnlyChangedLabels(t *testing.T) {
	c := NewCenter(Config{
		Enabled: true,
		Driver:  "default",
		Default: resilience.Config{Enabled: true, AttemptTimeout: dur(100)},
	})

	var redisN, gormN int
	c.Register("redis:cache", func(p resilience.Policy) { redisN++ })
	c.Register("gorm:mysql:primary", func(p resilience.Policy) { gormN++ })
	// Register arms once each.
	if redisN != 1 || gormN != 1 {
		t.Fatalf("after register: redis=%d gorm=%d, want 1/1", redisN, gormN)
	}

	// Change only redis:cache. gorm must NOT be notified (its policy unchanged).
	cfg := enabledTimeout(100)
	cfg.Rules = []Rule{{
		Resources: []string{"redis:cache"},
		Config:    resilience.Config{Enabled: true, AttemptTimeout: dur(200)},
	}}
	c.Refresh(cfg)
	if redisN != 2 {
		t.Fatalf("redis must be notified on its policy change: got %d, want 2", redisN)
	}
	if gormN != 1 {
		t.Fatalf("gorm must NOT be notified when its policy is unchanged: got %d, want 1", gormN)
	}
	if p := c.PolicyFor("redis:cache"); p.Timeout != dur(200) {
		t.Fatalf("post-refresh redis policy: want 200ms, got %v", p.Timeout)
	}

	// A no-op refresh (same config) notifies nobody.
	c.Refresh(cfg)
	if redisN != 2 || gormN != 1 {
		t.Fatalf("no-op refresh must not notify: redis=%d gorm=%d, want 2/1", redisN, gormN)
	}
}

func TestRefresh_DefaultChangeFansOutToAllUnoverridden(t *testing.T) {
	c := NewCenter(enabledTimeout(100))
	var a, b int
	c.Register("redis:cache", func(p resilience.Policy) { a++ })
	c.Register("gorm:mysql:primary", func(p resilience.Policy) { b++ })
	// Both read Default; changing Default must notify both.
	c.Refresh(enabledTimeout(300))
	if a != 2 || b != 2 {
		t.Fatalf("default change should fan out to both: a=%d b=%d, want 2/2", a, b)
	}
	if p := c.PolicyFor("redis:cache"); p.Timeout != dur(300) {
		t.Fatalf("post default-change: want 300ms, got %v", p.Timeout)
	}
}

func TestDriver(t *testing.T) {
	c := NewCenter(Config{Enabled: true, Driver: "sentinel"})
	if d := c.Driver(); d != "sentinel" {
		t.Fatalf("Driver: want sentinel, got %s", d)
	}
	// Defaults to "default" when unset.
	c2 := NewCenter(Config{Enabled: true})
	if d := c2.Driver(); d != "default" {
		t.Fatalf("Driver default: want default, got %s", d)
	}
}
