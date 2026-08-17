# patchutil
[English](README.md) | [中文](README_CN.md)

`patchutil` exposes a single reflection helper that clears the internal
read-only flag on a `reflect.Value`, allowing assignment to unexported
struct fields. It exists for framework-internal seams that cannot change
the target type, and is intended for **internal tooling and tests only**.
Part of Go-Spring's zero-dependency `stdlib` layer.

## Usage

```go
import (
    "reflect"
    "go-spring.org/stdlib/patchutil"
)

f := patchutil.PatchValue(reflect.ValueOf(&obj).Elem().FieldByName("secret"))
f.SetString("new value")
```

### API

- `PatchValue(v reflect.Value) reflect.Value` — returns the same `Value`
  with its `flagRO` bits cleared, so a following `Set` call succeeds even
  when the value originally addressed an unexported field.

## Design

Everything here trades "the alternative is worse" against "please do not
use this widely".

- **One unsafe primitive.** Only the RO flag is cleared — not a reflection
  library, not a general "modify anything" helper; addressability, kind,
  and other invariants stay the caller's job. Other reflection helpers
  live in `stdlib/typeutil` or the standard `reflect` package.
- **Layout dependence.** Uses `unsafe` against the exact memory layout of
  `reflect.Value` and the private `flagStickyRO` / `flagEmbedRO` bit
  values — stable across Go releases in practice, but outside the Go 1
  compatibility promise. `PatchValue` patches the flag word of the value
  it receives and returns that same `Value`; there is no locking, and the
  package is not thread-safe in any meaningful sense. Do **not** use in
  production business code.
- **Why it exists.** The package could be avoided by exporting the fields
  callers need; it survives because framework-internal seams (the
  container's injection code) sometimes cannot modify the target struct.
  That scope is small and audit-friendly, hence a ~40-line file.

## License

Apache License 2.0
