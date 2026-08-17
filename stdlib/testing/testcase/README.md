# testcase

[English](README.md) | [中文](README_CN.md)

`testcase` is the shared assertion suite that exercises the checks in `stdlib/testing/internal` through **both** [assert](../assert/) and [require](../require/). It is a test-only package (`package testcase_test`) that exports no code; it exists so the behaviour of the two entry points cannot drift apart. If you are looking for assertion helpers, use `assert` or `require` directly.

## Usage

There is no public API to import — the suite is discovered only by `go test`:

```
go test ./stdlib/testing/...
```

Six files, one per assertion family:

| File | Covers |
|------|--------|
| `assert_test.go` | Generic `That` and `Panic` |
| `error_test.go`  | `Error` (`Is`, `Matches`, `String`, ...) |
| `number_test.go` | `Number[T]` |
| `string_test.go` | `String` |
| `slice_test.go`  | `Slice[T]` |
| `map_test.go`    | `Map[K,V]` |

## Design

Running the same scenarios through both entry points guarantees that they expose the same methods with the same signatures, report failures with the same message shape, and differ only in whether the test stops.

- **Fake `TestingT`.** The suite drives assertions with `internal.MockTestingT`, which records failure output in a buffer instead of failing the outer test, so it can assert on the *content* of a failure message.
- **One test file per assertion family**, matching the `internal/*.go` breakdown: a change in one family maps to one test file.
- **No exported symbols, no production code.** The directory holds only `*_test.go` files, so tools scanning the module for public API see nothing here. The suite depends only on the Go standard library plus `testing/internal` and the two wrappers — a third-party dependency would leak into the wrapper packages through build-time coupling.
- **Shared suite over duplication; test-only package, not a helper library.** Duplicating the test bodies inside `assert` and `require` would drift as the modes are maintained separately, so one suite through both entry points forces parity. And publishing the fake `TestingT` and the scenario tables would tempt callers to embed them in their own tests, which the two-mode contract is not designed for.

## License

Apache License 2.0
