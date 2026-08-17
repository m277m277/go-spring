# typeutil
[English](README.md) | [中文](README_CN.md)

`typeutil` provides the reflection helpers the Go-Spring container uses to classify types — bean vs. property, constructor signatures, primitive vs. struct — when scanning autowire / provider targets. Centralising these predicates keeps the names that appear in container error messages grep-able from one file. Part of the zero-dependency `stdlib` layer.

## Usage

```go
import (
    "reflect"
    "go-spring.org/stdlib/typeutil"
)

if typeutil.IsConstructor(reflect.TypeOf(fn)) {
    // ok, register as a bean constructor
}
```

### API

Type constraints:

- `IntType`, `UintType`, `FloatType` — generic constraints over the respective Go primitive number families.

Reflection predicates on `reflect.Type`:

- `IsFuncType(t)` — is `t` a function type?
- `IsErrorType(t)` — is `t` exactly `error` or does it implement `error`?
- `ReturnNothing(t)` — function returns no values.
- `ReturnOnlyError(t)` — function returns exactly one value, which is an error.
- `IsConstructor(t)` — function returns either one non-error value or two values where the second is an error.
- `IsPrimitiveValueType(t)` — is it int / uint / float / string / bool?
- `IsPropBindingTarget(t)` — valid target for property binding (primitive, struct, or collection of those).
- `IsBeanType(t)` — chan, func, interface, or `*struct`.
- `IsBeanInjectionTarget(t)` — bean type or collection of beans.

## Design

- **Owns the vocabulary, not a reflection library.** "Primitive value type", "constructor", "bean type", "injection / binding target" are defined here in one file; anything that pokes at runtime values (rather than `reflect.Type`) belongs alongside its consumer.
- **`IsBeanType` shape:** `chan`, `func`, `interface`, `*struct`. Value structs are deliberately excluded — the container works with references so it can install proxies / advice; callers wanting value semantics wrap them in a pointer.
- **`IsConstructor` shape:** `func() T` (T not error) or `func() (T, error)`. Anything else (multiple returns, bare `func() error`) is rejected upstream by the container.
- **Two binding predicates, not one.** `IsPropBindingTarget` and `IsBeanInjectionTarget` stay separate because "give me a config value" and "give me a dependency" are different injection paths with different valid target shapes.
- **Partial nil guards.** Nil `reflect.Type` returns `false` in `IsErrorType` and `IsBeanInjectionTarget`, but not everywhere; callers with possibly-nil types should guard first.
- **Zero dependencies.** Everything is `reflect` plus generic constraints; the spring container's core internals (bean, arg, injecting, config binding) all import this package, so the bar for adding an import is very high.

## License

Apache License 2.0
