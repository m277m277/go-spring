/*
 * Copyright 2024 The Go-Spring Authors.
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

package log

import (
	"context"

	"go-spring.org/stdlib/goutil"
)

// PanicTag identifies panic reports in the log stream, so a deployment can
// route them to their own sink or alert without pattern-matching messages.
var PanicTag = RegisterInfraTag("panic", "")

// The panic bridge: importing this module replaces goutil's default stdout
// printer with a handler that reports every goutil-recovered panic as a
// structured error log line.
//
// goutil lives in stdlib, the layer below this module, so the dependency
// direction is the allowed one — stdlib itself must stay log-free, which is
// why the bridge is installed from here rather than defaulted in goutil.
//
// goutil.OnPanic is a single handler slot by design: this assignment wins
// over the printf default, and an application that assigns goutil.OnPanic
// itself replaces this bridge wholesale (imported packages initialize first,
// so the application's assignment always lands after this one). One slot,
// one owner — no hidden accumulation.
func init() {
	goutil.OnPanic = func(ctx context.Context, info goutil.PanicInfo) {
		Errorf(ctx, PanicTag, "panic recovered: %v\n%s", info.Panic, info.Stack)
	}
}
