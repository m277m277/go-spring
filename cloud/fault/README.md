# fault

[![Go-Spring](https://img.shields.io/badge/Go--pring-cloud-blue)](https://github.com/go-spring/go-spring)

`fault` is the in-process **fault-injection** companion to
[cloud/resilience](../resilience). It wraps a `resilience.Executor` so a
configurable fraction of operations are made to fail or slow down on demand —
"setting fire" to a running client — to verify that retry, circuit-breaker,
per-attempt timeout and Fallback actually engage, and that the observe kit
records the resulting outcomes.

## Features

- One seam for now: `WrapExecutor` — wraps an executor so injected faults land
  *inside* the retry/breaker loop (validating resilience, not bypassing it).
- Hot-reloadable config (`Injector.SetConfig`), driven from a starter's
  `gs.Dync[fault.Config]` binding — toggle fires at runtime, no restart.
- Three injection kinds: `generic` (a retryable injected error), `timeout`
  (`context.DeadlineExceeded`), `reset` (`syscall.ECONNRESET`); plus a pure
  latency mode.
- Injected errors implement `resilience.Retryable`, so they deterministically
  drive retries regardless of the host's retry predicate.
- stdlib + resilience only — no third-party deps, no gs/spring dependency.

## Install

```sh
go get go-spring.org/cloud
```

## Usage

Wrap an executor and drive it from config (redigo does this in `setupResilience`):

```go
import (
    "go-spring.org/cloud/fault"
    "go-spring.org/cloud/resilience"
)

rawExec, _ := resilience.NewExecutor(driver, policy)
inj := fault.NewInjector(fault.Config{Enabled: true, Rate: 0.5, Error: "generic"})
exec := fault.WrapExecutor(rawExec, inj)        // innermost
exec = resilobserve.WrapExecutor(exec, ...)     // observe outermost
```

Toggle at runtime:

```go
inj.SetConfig(fault.Config{Enabled: false})     // fire off
```

Config (per starter key prefix, e.g. redigo `${fault.*}`):

```properties
fault.enabled=true
fault.rate=0.5
fault.error=generic        # "" | "generic" | "timeout" | "reset"
fault.latency=50ms         # optional, applied to every call
```

See [DESIGN.md](DESIGN.md) for the injection-point rationale and boundaries, and
`starter-redigo/example-load` for a runnable load test that toggles fault.

## Status

First cut: `WrapExecutor` seam + redigo pilot. Dialer / RoundTripper seams and
per-resource rules are planned follow-ups (see [DESIGN.md §4](DESIGN.md)).
