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

package StarterTrpc

import (
	"context"

	"go-spring.org/cloud/governance/fault"
	"trpc.group/trpc-go/trpc-go/filter"
)

// FaultServerFilter is a tRPC ServerFilter that injects faults (latency/error)
// into inbound RPCs per the injector's rules. The starter registers it under
// the name "fault"; add "fault" to a service's filter chain to activate it. It
// is the tRPC server-side counterpart to the client starters'
// fault.WrapExecutor, letting an operator "set fire" to a running server. The
// injector is resolved from the neutral [fault.InjectorFor] seam (backed by the
// governance center) on each call; when no injector is registered fault.Apply is
// a transparent pass-through.
func FaultServerFilter() filter.ServerFilter {
	return func(ctx context.Context, req interface{}, next filter.ServerHandleFunc) (interface{}, error) {
		var resp interface{}
		err := fault.Apply(ctx, fault.InjectorFor(), "trpc", func() error {
			var e error
			resp, e = next(ctx, req)
			return e
		})
		return resp, err
	}
}
