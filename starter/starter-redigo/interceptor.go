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

package StarterRedigo

import "context"

// CommandInterceptor wraps one Redis command. It sits OUTERMOST in Conn's
// command path — before the observe span is started and before the resilience
// executor runs — so it can:
//   - short-circuit (e.g. a local-cache hit that must neither start a span nor
//     consume a breaker permit),
//   - rewrite args or swap the context,
//   - or simply observe the outcome by invoking next and forwarding its result.
//
// next is the full built-in path (observe span + executor + inner Do). Call it
// exactly once for the command to reach Redis; skip it to short-circuit. Errors
// returned by the built-in path — redis.ErrNil, resilience.ErrRateLimited /
// ErrCircuitOpen / ErrBulkheadFull — surface as next's return; the interceptor
// may translate or suppress them.
//
// Because the interceptor sits outside the executor, a short-circuit does NOT
// count toward the circuit breaker / rate limiter and does NOT emit a span. An
// observer-style interceptor that wants the command to count must call next.
//
// One interceptor applies to EVERY redigo instance in the process (mirrors
// starter-gin's single EngineMiddleware). Register it once via
// RegisterInterceptor; compose any branching (by command, by context value you
// set per pool, ...) inside that one function. A process with no registered
// interceptor runs the built-in path directly with zero overhead.
type CommandInterceptor func(
	ctx context.Context,
	cmd string,
	args []interface{},
	next func(ctx context.Context) (reply interface{}, err error),
) (reply interface{}, err error)

// interceptor is the single process-wide per-command hook, set via
// RegisterInterceptor and read when each pool is built (newPool). nil means no
// interceptor is in effect.
var interceptor CommandInterceptor

// RegisterInterceptor installs the single per-command interceptor applied to
// every redigo instance in this process. Call it once from an init() in the
// application, before gs.Run builds the pools. It panics on a second call —
// there is deliberately one slot; compose any per-command or per-instance
// branching inside the function you register. Mirrors the starter-gin single
// EngineMiddleware model.
func RegisterInterceptor(h CommandInterceptor) {
	if h == nil {
		panic("redigo: register nil interceptor")
	}
	if interceptor != nil {
		panic("redigo: interceptor already registered")
	}
	interceptor = h
}
