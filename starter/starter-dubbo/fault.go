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
	"sync/atomic"

	"go-spring.org/cloud/fault"
	"dubbo.apache.org/dubbo-go/v3/common/extension"
	"dubbo.apache.org/dubbo-go/v3/filter"
	"dubbo.apache.org/dubbo-go/v3/protocol/base"
	"dubbo.apache.org/dubbo-go/v3/protocol/result"
)

func init() {
	// Register the inbound fault-injection filter under "fault". It reads the
	// package-level injector (set via [SetFaultInjector]); until that is set it
	// is a transparent pass-through. Activate by calling SetFaultInjector and
	// adding "fault" to a provider's filter chain. dubbo's filter registry hands
	// out filters via a no-arg constructor, so the injector is plumbed through a
	// package-level pointer rather than a constructor argument.
	extension.SetFilter(faultFilterKey, newFaultFilter)
}

const faultFilterKey = "fault"

// faultInjector holds the live fault injector for the registered "fault"
// filter. nil (the zero value) means no fault injection — the filter passes
// every call through. Set via [SetFaultInjector].
var faultInjector atomic.Pointer[fault.Injector]

// SetFaultInjector installs the injector the "fault" filter injects with,
// letting an operator "set fire" to a running dubbo server. Pass nil to disarm.
// It is the dubbo server-side counterpart to the client starters'
// fault.WrapExecutor.
func SetFaultInjector(inj *fault.Injector) {
	faultInjector.Store(inj)
}

type dubboFaultFilter struct{}

func newFaultFilter() filter.Filter { return &dubboFaultFilter{} }

// Invoke gates the service call with [fault.Apply] when an injector is set.
func (f *dubboFaultFilter) Invoke(ctx context.Context, invoker base.Invoker, inv base.Invocation) result.Result {
	inj := faultInjector.Load()
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
