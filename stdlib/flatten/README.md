# flatten
[English](README.md) | [中文](README_CN.md)

`flatten` turns hierarchical JSON-shaped data into flat `key -> string` maps
and provides the storage abstraction the Go-Spring configuration binder reads
against. Part of the zero-dependency `stdlib` layer. It is not a config
loader: it never reads files, env, or CLI flags — the caller builds
`Properties` and slots it into a layer.

## Usage

```go
import "go-spring.org/stdlib/flatten"

flat := flatten.Flatten(map[string]any{
    "server": map[string]any{"port": 8080, "host": "localhost"},
    "users":  []any{map[string]any{"name": "tom"}},
})
// flat == {"server.port":"8080","server.host":"localhost","users[0].name":"tom"}

path, err := flatten.SplitPath("server.port")
_ = path // [{key server} {key port}]
_ = flatten.JoinPath(path)

s := flatten.NewPropertiesStorage(flatten.NewProperties(flat))
v, _ := s.Value("server.port")
```

### API

- `Flatten(map[string]any) map[string]string` — flatten nested maps and slices
  with dot / bracket notation (`a.b`, `a[0]`).
- `Path`, `JoinPath`, `SplitPath` — parse and render hierarchical keys.
- `Properties` / `PropertiesStorage` — flattened `key -> string` store plus a
  `Storage` interface adapter used by the binder.
- `PrefixedStorage` — transparent key prefix wrapper.
- `LayeredStorage` — multi-source configuration with fixed precedence layers
  (`StorageCommandLine`, `StorageEnvironment`, `StorageProfileFile`,
  `StorageAppFile`, `StorageDefault`).

### Flattening rules

- Nested maps expand with `.` (`{"a":{"b":1}}` -> `"a.b"="1"`).
- Slices expand with `[i]` (`{"a":[1,2]}` -> `"a[0]"="1"`, `"a[1]"="2"`).
- Untyped and typed `nil` values become `"<nil>"`.
- Empty (non-nil) maps become `"{}"`, empty slices become `"[]"`.
- Primitive values use `strconv` formatting.

## Design

- `Flatten` is display-oriented and one-way: **not reversible**. It feeds
  logging, diffing, diagnostics, and `Storage` input, and supports only
  JSON-native types (map/slice/primitive/nil); structs, non-string map keys,
  and custom types are out of scope by design.
- `Path` + `Split/JoinPath` are the round-trippable key representation;
  `Storage` stays minimal — `Value` / `MapKeys` / `SliceEntries` plus `Exists`
  for property-condition checks — so alternative implementations (e.g. remote
  config) can plug in.
- `LayeredStorage` deliberately mixes two override rules: leaf values and
  slices are highest-layer-wins, so lower-layer partial slices are hidden
  entirely once a higher layer defines the slice (`my.list[0]=c` over `[a,b]`
  yields `[c]`, not `[c,b]`); map keys merge across every layer with per-leaf
  override resolution. The asymmetry is intentional — merging arrays would be
  ambiguous, merging map keys is the shape callers expect.
- `PrefixedStorage.SliceEntries` re-strips its own prefix from returned keys
  so callers stay in their namespace; `LayeredStorage.Data()` is a snapshot
  for introspection (e.g. an actuator "env" endpoint), not a binding path.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
