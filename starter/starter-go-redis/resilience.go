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

package StarterGoRedis

import (
	"context"
	"errors"
	"io"

	"github.com/redis/go-redis/v9"
	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	observe "go-spring.org/observe"
	"go-spring.org/observe/resilience"
	"go-spring.org/spring/gs"
)

// Client is the wrapper bean go-redis clients are injected as. It
// embeds the concrete redis.UniversalClient (a *redis.Client or *redis.ClusterClient
// depending on mode, so methods promote unchanged) and field-injects the resilience
// policy via gs.Dync so it hot-reloads on config change. newClient returns one; gs
// field-injects Resilience + Observability, then calls Init (InitMethod).
type Client struct {
	redis.UniversalClient
	Resilience    gs.Dync[resilience.Config] `value:"${resilience:=}"`
	Fault         gs.Dync[fault.Config]      `value:"${fault:=}"`
	Observability observe.ObserveConfig      `value:"${observability:=}"`

	cfg      Config // for resourceLabel (address fields)
	exec     resilience.Executor
	faultInj *fault.Injector
	resource string
	stop     io.Closer // driver-supplied teardown (e.g. discovery resolver watch)
}

// Init is the gs InitMethod (runs after gs field-injects Resilience +
// Observability). It builds the executor when resilience is enabled, attaches the
// per-command hook, and subscribes to policy changes for hot Refresh.
func (o *Client) Init() error {
	rc := o.Resilience.Value()
	fc := o.Fault.Value()
	if !rc.Enabled && !fc.Enabled {
		return nil
	}
	rawExec, err := resilience.NewExecutor(rc.Driver, rc.Policy())
	if err != nil {
		return err
	}
	exec := rawExec
	if fc.Enabled {
		o.faultInj = fault.NewInjector(fc)
		exec = fault.WrapExecutor(rawExec, o.faultInj)
	}
	exec = resilobserve.WrapExecutor(exec, "redis", o.Observability)
	o.exec = exec
	o.resource = resourceLabel(o.cfg)
	o.AddHook(&resilienceHook{exec: exec, resource: o.resource})
	o.Resilience.OnChanged(func(new, _ resilience.Config) {
		_ = exec.Refresh(new.Policy())
	})
	if o.faultInj != nil {
		o.Fault.OnChanged(func(new, _ fault.Config) {
			o.faultInj.SetConfig(new)
		})
	}
	// Attach the access-log Hook (trace+metric come from redisotel above). It
	// rides the observe kit and defaults to a no-op pass-through when off.
	applyObservability(o.Observability, o.UniversalClient)
	return nil
}

// Destroy is the gs destroy method: closes the resilience executor (if armed),
// stops any driver-supplied teardown (the discovery resolver watch, when armed),
// and closes the underlying client.
func (o *Client) Destroy() error {
	if o.exec != nil {
		_ = o.exec.Close()
	}
	_ = o.stop.Close()
	return o.UniversalClient.Close()
}

// resourceLabel derives a stable, human-readable resilience resource key for a
// client, so limiter and breaker state is scoped per Redis instance rather than
// per command. It falls back across the mode-specific address fields via the
// shared [resilience.ResourceLabel] helper.
func resourceLabel(c Config) string {
	first := ""
	if len(c.Addrs) > 0 {
		first = c.Addrs[0]
	}
	return resilience.ResourceLabel("redis", c.ServiceName, c.MasterName, c.Addr, first)
}

// resilienceHook routes every Redis command (and pipeline) through the executor.
// DialHook is left untouched — connection establishment is discovery's concern,
// not the command-level protection we add here.
type resilienceHook struct {
	exec     resilience.Executor
	resource string
}

var _ redis.Hook = (*resilienceHook)(nil)

func (h *resilienceHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *resilienceHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return h.guard(ctx, cmd, func(ctx context.Context) error {
			return next(ctx, cmd)
		})
	}
}

func (h *resilienceHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		var setErr = func(err error) {
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
		}
		return h.run(ctx, setErr, func(ctx context.Context) error {
			return next(ctx, cmds)
		})
	}
}

// guard runs a single command through the executor, tagging the command with a
// rejection error when the limiter or breaker short-circuits it.
func (h *resilienceHook) guard(ctx context.Context, cmd redis.Cmder, call func(context.Context) error) error {
	return h.run(ctx, cmd.SetErr, call)
}

// run is the shared body for both command and pipeline hooks. It executes call
// under the policy, translating rejections into the command error and — crucially
// — treating redis.Nil (a cache miss / "key not found") as a success so it never
// trips the circuit breaker.
func (h *resilienceHook) run(ctx context.Context, setErr func(error), call func(context.Context) error) error {
	var callErr error
	execErr := h.exec.Execute(ctx, h.resource, func(ctx context.Context) error {
		callErr = call(ctx)
		if callErr != nil && !errors.Is(callErr, redis.Nil) {
			return callErr // a real failure feeds the breaker/retry
		}
		return nil // success or cache miss
	})
	if execErr != nil {
		if errors.Is(execErr, resilience.ErrRateLimited) || errors.Is(execErr, resilience.ErrCircuitOpen) {
			// Rejected before the command ran: surface the rejection to the caller.
			setErr(execErr)
			return execErr
		}
		// A non-nil executor error that is not a protection rejection. On the
		// normal failure path it equals callErr (the closure returned it) and is
		// already recorded on the command by go-redis. They diverge only when the
		// closure body never ran — e.g. a fault injector (cloud/fault) short-
		// circuited the attempt before reaching call — leaving callErr nil while
		// the executor still returns the injected error. Prefer execErr and tag
		// the command so the failure is not silently swallowed as success.
		if callErr == nil {
			setErr(execErr)
			return execErr
		}
		return callErr
	}
	return callErr
}
