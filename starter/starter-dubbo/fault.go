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

package StarterDubbo

import (
	"context"

	"dubbo.apache.org/dubbo-go/v3/common/extension"
	"dubbo.apache.org/dubbo-go/v3/filter"
	"dubbo.apache.org/dubbo-go/v3/protocol/base"
	"dubbo.apache.org/dubbo-go/v3/protocol/result"
	"go-spring.org/cloud/governance/fault"
)

func init() {
	// Register the inbound fault-injection filter under "fault". Add "fault" to
	// a provider's filter chain to activate it. The injector is resolved from the
	// neutral [fault.InjectorFor] seam (backed by the governance center) on each
	// call; when no injector is registered fault.Apply is a transparent pass-
	// through, so the filter is always safe to install. dubbo's filter registry
	// hands out filters via a no-arg constructor, so the injector is resolved at
	// call time rather than passed through a constructor argument.
	extension.SetFilter(faultFilterKey, newFaultFilter)
}

const faultFilterKey = "fault"

type dubboFaultFilter struct{}

func newFaultFilter() filter.Filter { return &dubboFaultFilter{} }

// Invoke gates the service call with [fault.Apply] when an injector is
// registered. The injector is resolved from [fault.InjectorFor] on each call so
// fault can be hot-toggled at runtime without a restart; nil means no fault is
// configured and the call passes through untouched.
func (f *dubboFaultFilter) Invoke(ctx context.Context, invoker base.Invoker, inv base.Invocation) result.Result {
	inj := fault.InjectorFor()
	if inj == nil {
		return invoker.Invoke(ctx, inv)
	}
	var res result.Result
	_ = fault.Apply(ctx, inj, "dubbo", func() error {
		res = invoker.Invoke(ctx, inv)
		return nil
	})
	if res != nil {
		return res
	}
	// Apply injected an error before invoker ran; surface a failing result.
	return &result.RPCResult{Err: fault.ErrInjected}
}

// OnResponse is a pass-through.
func (f *dubboFaultFilter) OnResponse(_ context.Context, res result.Result, _ base.Invoker, _ base.Invocation) result.Result {
	return res
}
