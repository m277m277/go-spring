# cache Design
[English](DESIGN.md) | [中文](DESIGN_CN.md)

`cache` is the caching abstraction of the data layer: the `ByteCache`
interface every backend implements, wrapped by a `Cache` struct façade that
layers a codec, plus a driver registry and a config-driven module that wires
a named backend into a bean.

## 1. Responsibilities & Boundaries

- **Does:** define the `ByteCache` interface (the bytes-native backend contract)
  and the `Cache` struct façade that wraps it; provide the `Codec`/`JSONCodec`
  serialization seam and the `ResolveCodec` default; run a `spring.cache`
  module that reads config and dispatches to registered drivers; expose the
  `RegisterDriver`/`GetDriver` registry.
- **Refuses:**
  - No concrete backend. Redis / redigo / bigcache / memcached adapters live
    in their starters; each registers a driver and wraps its own client bean.
  - No in-process `Memory`, no `MultiLevel`, no aspect bridge here. (An older
    experimental revision had them; they were dropped to keep the package
    focused on bytes-native backends.)
  - No stampede protection, async refresh, or negative caching — policies for
    the caller, not a general interface.

## 2. Key Abstractions / Seams

- **`ByteCache` is the bytes-native backend contract.** `GetBytes`/`SetBytes`/
  `Delete` are the raw primitives a remote backend (Redis, memcached, bigcache)
  maps 1:1 to its client; a backend implements only these three.
- **`Cache` is a struct façade that layers a codec.** `Cache` embeds a
  `ByteCache` and adds `Get`/`Set`, which marshal/unmarshal through a pluggable
  `Codec` (default `JSONCodec`) on top of the raw bytes; the `ByteCache`
  methods are promoted unchanged for callers that hold bytes already.
  `ResolveCodec` centralizes the default in the one place (the `Cache` methods)
  that needs it, so backends never re-derive it.
- **`ErrMiss` separates miss from failure.** A backend maps its native
  "key absent" (redis.Nil, memcache.ErrCacheMiss, bigcache.ErrEntryNotFound)
  to `ErrMiss`; any other error is a real failure. Callers fall through to the
  source of truth only on `ErrMiss`.
- **A Driver is a bean-builder factory.** `Driver func(beanID string)
  gs.ModuleFunc` takes the backend client's bean name and returns the module
  that provides the `Cache` bean — so this package never imports a concrete
  client type.
- **Config dispatch.** The package's own `init` module reads `spring.cache`,
  parses each `driver = "<driver>:<beanID>"`, looks up the driver, and invokes
  `driver(beanID)`. The beanID both selects the backend client and names the
  resulting cache bean.

## 3. Constraints

- The registry is safe to populate at init and read concurrently at runtime;
  one `sync.RWMutex` guards it.
- `ByteCache` implementations must be safe for concurrent use.
- A backend that cannot honor per-entry TTL must ignore the `ttl` argument
  silently and document it (bigcache uses a global `LifeWindow`) — never panic.
- `driver` parsing requires a non-empty driver name and beanID; anything else
  is a config error returned from the module, not a panic.

## 4. Trade-offs / Alternatives Rejected

- **Split the backend contract (`ByteCache`) from the typed façade (`Cache`).**
  A backend implements only the three bytes-native methods; the codec logic
  lives once on the `Cache` struct methods, not duplicated per backend. `Cache`
  embeds `ByteCache`, so the raw methods are promoted unchanged — callers see
  one type with the full surface, backends implement the narrowest one. The
  cost — a future live-value in-process backend would have to serialize — is
  accepted; use bigcache for an in-process tier.
- **Per-call codec, not per-instance.** `Get`/`Set` take an optional codec so
  one cache can serve mixed types; the default is JSON and a mismatched codec
  fails loudly on decode rather than corrupting silently. The struct form would
  make a per-instance codec field cheap, but per-call covers the common case
  without it.
- **Cache bean named by backend beanID.** The config slot (`spring.cache.X`)
  is only an iteration key; the bean name is the beanID suffix, coupling cache
  identity to the backend client. Intentional: one client → one cache name,
  and duplicate wiring panics at init rather than silently overwriting.
- **No aspect bridge in this package.** The old `AsStore` coupled this package
  to `aspect` and its `Store` contract. Dropping it keeps the package focused;
  an aspect adapter, if needed, belongs with `aspect` or the caller.
