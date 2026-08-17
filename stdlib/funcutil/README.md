# funcutil
[English](README.md) | [中文](README_CN.md)

`funcutil` returns runtime metadata (file, line, name) about a Go function
value, used by the container / aspect framework for human-readable diagnostics
("bean registered at file:line by funcX"). Part of the zero-dependency
`stdlib` layer: a two-function wrapper over `reflect` +
`runtime.FuncForPC`, not a stack walker — it only operates on a value passed
in by the caller.

## Usage

```go
import "go-spring.org/stdlib/funcutil"

func Handle() {}

name := funcutil.FuncName(Handle)
file, line, _ := funcutil.FileLine(Handle)
```

`fn` must be a function or method value. Passing anything else will panic
inside `reflect`.

### API

- `FuncName(fn any) string` — package-qualified function name, without the
  full module path prefix. Method values printed by the runtime as `T.m-fm`
  have the exact `-fm` suffix trimmed; names that merely end in `-`, `f` or
  `m` are left intact.
- `FileLine(fn any) (file string, line int, fnName string)` — the source
  location plus the cleaned-up name.

## Design

- The `-fm` strip uses `strings.TrimSuffix`, so the returned name is what
  humans would write; only the full method-value suffix goes away.
- The last `/`-separated segment is preserved (`pkg.Fn`), the module path
  prefix is dropped — a display choice that can bite comparisons if a caller
  assumes uniqueness across packages with the same short name.
- No caching: reflecting a `*runtime.Func` on every call is cheap enough for
  the current uses (registration time, diagnostics), and skipping the cache
  keeps this a two-function file.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
