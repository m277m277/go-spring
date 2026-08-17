# ordered
[English](README.md) | [中文](README_CN.md)

`ordered` currently exposes a single helper for deterministic iteration order
over a map. Part of Go-Spring's zero-dependency `stdlib` layer; a named
location for future "deterministic order" helpers — not an ordered-map data
structure, since Go's built-in map plus this helper is enough for the current
use cases.

## Usage

```go
import "go-spring.org/stdlib/ordered"

for _, k := range ordered.MapKeys(m) {
    fmt.Println(k, m[k])
}
```

### API

- `MapKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K` — sorted slice of the
  map's keys.

Used inside the framework whenever log output, JSON marshaling, or diagnostic
dumps need a stable key order.

## Design

- One call for stable-order map iteration — no per-caller slice +
  `sort.Strings`.
- The `cmp.Ordered` constraint (Go 1.21+) covers numeric and string keys
  without duplicated helpers; `slices.Sort`, not `sort.Strings`, keeps it
  generic.
- The returned slice is a copy; the caller may mutate it freely.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
