# mathutil
[English](README.md) | [中文](README_CN.md)

`mathutil` provides generic overflow checks for narrowing an `int64` /
`uint64` / `float64` into a smaller numeric type. Part of Go-Spring's
zero-dependency `stdlib` layer; it currently holds only the overflow checks
JSON / form binding need — not a general-purpose numeric library. No bounded
/ saturating conversions; callers get a bool and pick their own error
behaviour.

## Usage

```go
import "go-spring.org/stdlib/mathutil"

if mathutil.OverflowInt[int16](v) {
    return errors.New("out of range")
}
```

### API

- `OverflowInt[T ~int|~int8|~int16|~int32|~int64](v int64) bool`
- `OverflowUint[T ~uint|~uint8|~uint16|~uint32|~uint64](v uint64) bool`
- `OverflowFloat[T ~float32|~float64](v float64) bool`

Each returns `true` when the value cannot be represented in `T`. Used by
`stdlib/formutil` and `stdlib/jsonflow` when decoding numbers into narrower
target types.

## Design

- Dispatch switches on `unsafe.Sizeof` of `T`'s zero value; compile-time
  dispatch would push a per-type function set onto every call site —
  tolerated since the check runs only at decode boundaries.
- `OverflowInt[int64]` / `OverflowUint[uint64]` are no-ops by design: callers
  pass `strconv`-produced values, and the "no truncation needed" case must be
  cheap.
- `OverflowFloat[float64]` always returns `false`; `OverflowFloat[float32]`
  compares against `±math.MaxFloat32`; subnormals and NaN are not overflow.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
