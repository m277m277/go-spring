# listutil

[English](README.md) | [中文](README_CN.md)

`listutil` gives Go's `container/list` a generic, type-safe skin and adds a few
convenience helpers for slices and writers. Part of the zero-dependency `stdlib`
layer: every method is a one-liner over the embedded stdlib type, plus the
small utilities that show up repeatedly in framework code.

## Usage

Import path: `go-spring.org/stdlib/listutil`.

```go
import "go-spring.org/stdlib/listutil"

l := listutil.New[int]()
l.PushBack(1)
l.PushBack(2)

for e := l.Front(); e.Valid(); e = e.Next() {
    _ = e.Value() // int
}
```

API surface:

- `List[T]` / `Element[T]` — thin generic wrappers over `container/list.List`
  / `list.Element`, keeping the doubly-linked-list API but returning typed
  values instead of `any`.
- Helper functions:
  - `SliceOf[T](items ...T) []T` — sugar for building a slice from varargs.
  - `ListOf[T](items ...T) *list.List` — build a `*list.List` from varargs.
  - `AllOfList[T](l *list.List) []T` — collect all elements as `[]T`
    (panics if the list holds a different type).
  - `WriteStrings(w io.Writer, values ...string) error` — write strings in
    order, stopping at the first error.

## Design

- **Restore compile-time typing, not a rewrite**: every method forwards to the
  embedded `container/list` type; not a linked-list rewrite, not a functional
  collections library.
- **`Element[T]` embeds `*list.Element` by pointer** so `Valid()` can safely
  compare it to nil; the zero value is a valid "nil" end-of-iteration marker.
- **`AllOfList` panics on mixed types** — deliberate: it uses `e.Value.(T)`. A
  type-checked variant would need an `ok, err` shape most callers don't want.
- **No foreign-list check**: the wrapper does not verify an `Element[T]` comes
  from the same list — `container/list` itself panics there, no extra check.

## License

Apache License 2.0
