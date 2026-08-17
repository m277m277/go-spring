# require
[English](README.md) | [中文](README_CN.md)

`require` provides fluent, type-specific assertions that **stop the test on the first failure** — no further assertions run. The sibling package [`assert`](../assert/) has identical semantics but continues on failure. See the parent package [`testing`](../) for the full assertion reference and the `assert` / `require` comparison.

## When to use `require` over `assert`

Use `require` when a failed check invalidates everything after it — e.g. the value being unwrapped is nil, or the fixture failed to set up. Continuing would either panic or produce nonsensical failure output. Use `assert` when multiple independent assertions in one test are informative on their own.

## Usage

```go
package myapp_test

import (
    "testing"

    "go-spring.org/stdlib/testing/assert"
    "go-spring.org/stdlib/testing/require"
)

func TestUser(t *testing.T) {
    user := loadUser()
    require.That(t, user).NotNil() // stop here if nil — the next line would panic

    assert.String(t, user.Email).IsEmail()
    assert.Number(t, user.Age).GreaterThan(0)
}
```

### Features

- Same fluent API as `assert`: `That`, `Error`, `Number[T]`, `String`, `Slice[T]`, `Map[K,V]`, top-level `Panic`.
- Fails with `t.Fatal` — the test stops immediately.
- Every method accepts trailing `msg ...string` for custom failure messages.
- Zero third-party dependencies.

## Design

`require` is a **thin wrapper** over the shared assertion engine in `stdlib/testing/internal`, differing from [`assert`](../assert/) only in that a failing check calls `t.Fatal` instead of `t.Error`. It exists so the fail-fast choice is visible at the import statement.

- **One constant decides the mode.** `const fatalOnFailure = true` is the single line that makes this package "require" instead of "assert"; every entry point passes it through.
- **No assertion logic of its own.** Every check lives in `internal` and is shared with `assert`; the fluent value objects (`*internal.Assertion`, ...) are identical to `assert`'s, so the two modes are drop-in replacements from a signature perspective.
- **`internal.TestingT` seam.** The minimal `*testing.T` surface (`Helper` / `Error` / `Fatal`) accepted by every entry point.
- **Constraints.** No dependency beyond `internal`; parity with `assert` is enforced by the `testcase` suite running the same scenarios through both entry points. `t.Fatal` stops only the current test — a `t.Run` subtest that fails through `require` stops itself, and the parent test decides whether to continue.
- **Rejected alternatives.** A runtime mode flag was rejected because the reader would have to trace whether some earlier `SetMode(...)` call had flipped the behaviour; depending on testify was rejected — the API surface is kept close for muscle memory, but the implementation is stdlib-only.

## License

Apache License 2.0
