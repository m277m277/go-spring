# assert
[English](README.md) | [中文](README_CN.md)

`assert` provides fluent, type-specific assertions that **do not stop the test on failure** — subsequent assertions still run, so a single test can report multiple issues at once. The sibling package [`require`](../require/) has identical semantics but stops on the first failure.

See the parent package [`testing`](../) for the full assertion reference and the comparison between `assert` and `require`.

## Usage

```go
package myapp_test

import (
    "testing"

    "go-spring.org/stdlib/testing/assert"
)

func TestUser(t *testing.T) {
    assert.That(t, "hello").Equal("hello")
    assert.Number(t, 42).GreaterThan(40)
    assert.String(t, "user@example.com").IsEmail()
    assert.Slice(t, []int{1, 2, 3}).Contains(2)

    // Failure here does not stop the test — the next assertion still runs.
    assert.That(t, "a").Equal("b")
    assert.That(t, "c").Equal("c") // will still execute
}
```

### Features

- Generic entry points: `That`, `Error`, `Number[T]`, `String`, `Slice[T]`, `Map[K,V]`, top-level `Panic`.
- Fluent chained checks (e.g. `.Equal(...)`, `.NotNil()`, `.Contains(...)`).
- Every method accepts trailing `msg ...string` for custom failure messages.
- Zero third-party dependencies.

## Design

`assert` is a **thin wrapper** over the shared assertion engine in `stdlib/testing/internal`, making the "fail-continue" mode a compile-time choice at the import statement rather than a runtime flag on every call.

- **One constant decides the mode.** `const fatalOnFailure = false` is the single line that makes this package "assert" instead of "require"; every entry point passes it through, and a failing check reports via `t.Error` (continue) rather than `t.Fatal` (stop).
- **No assertion logic of its own.** Every check lives in `internal` and is shared with `require`; the fluent value objects (`*internal.Assertion`, `*internal.ErrorAssertion`, ...) are the same types both modes use, so the modes cannot diverge in behaviour.
- **`internal.TestingT` seam.** The minimal `*testing.T` surface (`Helper` / `Error` / `Fatal`) accepted by every entry point — the same assertion runs under a real test, a subtest, or the `MockTestingT` harness the `testcase` suite uses to introspect failures.
- **Constraints.** No dependency beyond `internal` (new deps would ripple into every module's test binary); parity with `require` is enforced by the `testcase` suite running the same scenarios through both entry points. `Panic` is a top-level function, not a fluent chain — its target is a callback, not a value.
- **Rejected alternatives.** One package plus a runtime mode flag would let a test accidentally mix modes in one function; splitting by import keeps the choice visible at every call site. Depending on testify was rejected — the API is intentionally close for muscle memory, but the implementation is stdlib-only.

## License

Apache License 2.0
