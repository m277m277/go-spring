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

// This file wires the governance center into the gs container and the resilience
// executor seam. It is what makes importing cloud/govern sufficient to turn
// ${govern} config into live, hot-reloaded resilience policy: a single
// gs.Dync[govern.Config] feeds a *Center, which (via a gs.Runner) registers
// itself as the process-wide executor provider behind resilience.ExecutorFor.
//
// cloud/govern is no longer gs-free because of this file — it imports spring/gs.
// The pure-logic types above (Config, Center, PolicyFor, Refresh) stay free of
// container concerns; this file is the single gs touch point. cloud/resilience
// remains independent of cloud/govern, so there is no import cycle.

package govern

import (
	"context"
	"sync"

	"go-spring.org/cloud/resilience"
	"go-spring.org/spring/gs"
)

func init() {
	// centerHolder owns the single gs.Dync that feeds governance. It is the
	// only bean that binds a Dync; every client reads a resolved policy from
	// the *Center it builds (or, more commonly now, via resilience.ExecutorFor).
	//
	// Exported as a gs.Runner so gs collects and instantiates it even when no
	// client injects *Center — the common case now that clients resolve their
	// executor through the neutral resilience.ExecutorFor seam. Without a
	// collected-type export gs would not instantiate an unreachable bean, so the
	// provider would never be registered; the Runner export makes importing
	// cloud/govern sufficient on its own.
	gs.Provide(func() *centerHolder { return &centerHolder{} }).
		Init((*centerHolder).Init).Destroy((*centerHolder).Destroy).
		Export(gs.As[gs.Runner]()).Caller(1)

	// Expose *Center as a bean for clients that read policy fields directly
	// (only starter-dubbo today, whose URL-param governance model bypasses the
	// executor seam). gs wires the holder (and runs its Init, building the
	// center) before this ctor runs: this bean depends on *centerHolder, so the
	// holder reaches StatusWired (Init included) first, and h.center is non-nil.
	gs.Provide(func(h *centerHolder) *Center { return h.center }).Caller(1)
}

// centerHolder is the single owner of the governance gs.Dync and the single
// OnChanged subscription. The Dync is field-injected (the only way gs can bind
// a gs.Dync to a ${...} key); the *Center it builds in Init is the public bean.
type centerHolder struct {
	// Gov is the single source of truth for governance. Field-injected and
	// hot-reloaded: every client's resilience policy flows from this one binding.
	Gov gs.Dync[Config] `value:"${govern:=}"`

	center *Center

	// labelExecs memoizes the executor built for each resource label so the
	// governance subscription (Center.Register) is armed exactly once per label,
	// even if resilience.ExecutorFor hands the label to the provider concurrently
	// on first use. The cloud/resilience cache also memoizes for lookup speed;
	// this one is specifically for Register-once.
	labelExecs sync.Map // label -> resilience.Executor
}

// Init builds the center from the bound config and subscribes the ONE OnChanged
// handler that fans policy changes out to every registered client.
func (h *centerHolder) Init() error {
	h.center = NewCenter(h.Gov.Value())
	h.Gov.OnChanged(func(new, _ Config) {
		h.center.Refresh(new)
	})
	return nil
}

// Run implements gs.Runner. gs collects every Runner bean and runs it at
// startup, after injection — the hook that makes importing cloud/govern
// sufficient on its own: the executor provider is registered here, so client
// adapters that call resilience.ExecutorFor get a governance-backed executor
// without any of them injecting *Center.
func (h *centerHolder) Run(_ context.Context) error {
	resilience.RegisterExecutorProvider(h.executorFor)
	return nil
}

// executorFor is the governance-backed provider registered with
// resilience.RegisterExecutorProvider. For a resource label it builds the
// executor the center resolves (the center's driver + the label's policy) and
// subscribes it to policy changes — so a hot-reload of ${govern} refreshes the
// executor in place. Memoized per label so the subscription is armed once.
func (h *centerHolder) executorFor(label string) resilience.Executor {
	if v, ok := h.labelExecs.Load(label); ok {
		return v.(resilience.Executor)
	}
	exec, err := resilience.NewExecutor(h.center.Driver(), h.center.PolicyFor(label))
	if err != nil || exec == nil {
		return nil // resilience.resolve falls back to a no-op executor
	}
	h.center.Register(label, func(p resilience.Policy) { _ = exec.Refresh(p) })
	actual, _ := h.labelExecs.LoadOrStore(label, exec)
	return actual.(resilience.Executor)
}

// Destroy is a no-op today: the center holds only in-memory subscribers and an
// atomic snapshot, with no background goroutines or closeable resources. It
// exists so the lifecycle is symmetric and a future background pump can hook it.
func (h *centerHolder) Destroy() error { return nil }
