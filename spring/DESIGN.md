# spring Design
[English](DESIGN.md) | [中文](DESIGN_CN.md)

`go-spring.org/spring` is the **core layer** of the Go-Spring stack
(stdlib → log → spring → cloud → starter). It provides the IoC container,
dependency-injection wiring, the layered configuration engine, and the
application lifecycle model — and nothing else. It depends on `stdlib` and
`log` only; it never imports the `cloud` ecosystem library, a third-party
business SDK, or any starter.

## 1. Responsibilities & Boundaries

- The container's job is: collect bean definitions during `init()`, resolve
  conditions, wire dependencies, run the `Init` / `Destroy` lifecycle, and
  drive `Runner` / `Server` roles from `gs.Run()`. The container refuses to
  own protocol logic or third-party clients — it exposes seams (`Provide`,
  `Module`, `Group`, `Condition`) that starters use to contribute those.
- `spring/conf` is the configuration engine: layered sources (command line,
  env, `app-<profile>.<ext>`, `app.<ext>`, in-memory, tag defaults) merged
  by priority, format readers under `spring/conf/reader/{yaml,toml,prop,json}`,
  and pluggable decryption under `spring/conf/decrypt`. The engine is
  independent of the container; the container drives it during boot. It is
  also the only spring package the outside ecosystem reuses directly —
  binding glue such as starter-governance's rules parser builds on it.
- `spring/gs` is the public surface: `Provide`, `Configure`, `Module`,
  `Group`, `OnProperty`, `OnBean`, `Dync[T]`, `Runner`, `Server`,
  `ReadySignal`, `PropertiesRefresher`. Everything else in
  `spring/gs/internal/...` is implementation detail and off-limits to users.

### Where the capability families live

`spring/` itself contains only `gs/` and `conf/` — the pure core. Every
capability abstraction moved out to the layer its dependency direction
demands:

| Family | Home | Why |
|---|---|---|
| `httpsvr`, `httpclt` | `stdlib/` | pure HTTP semantics, no container dependency |
| governance (`resilience`, `fault`, `traffic`), `discovery`, `loadbalance`, `event`, `scheduling`, `batch`, `lock`, `messaging`, `transaction`, `tlsconf`, `cache`, `repository`, `migration`, `i18n`, `validation`, `httpx`, `security`, `session` | `cloud/` (go-spring.org/cloud) | ecosystem abstractions, container-free — the cloud module imports no spring package |
| concrete backends (Redis, GORM, Kafka, dubbo-go, …) | `starter/` | third-party SDKs + gs wiring |

The rule the whole repo follows: **abstractions go in `cloud`, gs wiring and
third-party SDKs go in a starter, pure semantics with no ecosystem dependency
go in `stdlib`** — so a starter wires, cloud abstracts, spring runs.

## 2. Key Abstractions & Seams

- **Bean registration.** `gs.Provide(objOrCtor, args...)` records a bean
  definition at `init()` time. Constructors are functions; their parameters
  are matched against the type index. Chainable builders configure `Name`,
  `Init` / `Destroy`, `Condition`, `DependsOn`, `Export`, `Configuration`.
- **Type-index-by-exported-interface.** The container indexes each bean
  twice: under its own concrete type, and under every interface passed to
  `.Export(gs.As[Iface]())`. Interfaces not listed in `Export` are **not**
  indexed — `[]Iface autowire:""` and `gs.OnBean[Iface]()` won't find the
  bean even though it structurally satisfies `Iface`. See
  `spring/gs/internal/gs_core/injecting/injecting.go` (`beansByType`,
  `GetExports`). A bean must also be a reference type (pointer / interface);
  value structs cannot be beans. Multiple beans of the same type must call
  `.Name()` or duplicate-bean resolution fails.
- **Dependency injection.** Struct fields tagged `autowire:""` /
  `autowire:"name?"` / `autowire:"a,*?,b"` and `value:"${key:=default}"`
  are populated during a single reflection pass. After boot, no reflection
  runs — matched functions and field offsets are cached.
- **Conditional auto-config.** `gs.Module(cond, fn)` groups a starter's
  beans behind a `PropertyCondition`; `gs.OnProperty` / `gs.OnBean` /
  `gs.OnMissingBean` / `gs.OnSingleBean` compose via `And` / `Or` / `Not` /
  `None`. This is the seam every starter uses to opt itself in from a
  configuration key.
- **Dynamic configuration.** `gs.Dync[T]` wraps a field so
  `PropertiesRefresher.RefreshProperties()` re-binds it in place without a
  container restart. This is the shared seam that config-center starters
  (`starter-config-{nacos,etcd,consul,vault,file}`) plug into.
- **Runtime model.** `Runner` (one-shot) and `Server` (long-running with
  `ReadySignal`) are the only two roles. All servers listen first, then
  block on `sig.TriggerAndWait()` so no server accepts traffic before every
  server is bound. The framework owns concurrent start, signal handling,
  and graceful `Stop()`.

## 3. Constraints

- No third-party business dependency in `spring/`. Anything past the Go
  standard library, `stdlib/`, and `log/` belongs in `cloud/` (ecosystem
  abstractions) or `starter/` (backends + gs wiring).
- All registration happens at `init()` time. `Configure(func(app gs.App))`
  extends that phase; nothing may register beans after `Run` starts.
- The `internal/` subtree is not part of the public API surface, even
  though it is reachable through re-exports; downstreams must consume the
  `gs.` package.
- A bean's exported interfaces are exactly what `.Export(gs.As[Iface]())`
  declares — no automatic interface discovery. Missing an `Export` is the
  most common wiring bug and it fails silently at collection time.

## 4. Trade-offs & Alternatives Rejected

The rejections below are one stance, not separate calls: **the container
assembles; it does not adjudicate.** Java Spring's extension-point growth is
driven by forces this project does not share — hooks serving the thousands
of frameworks hosted *on* Spring (a BeanPostProcessor exists so proxies can
replace user beans), complexity moved into the container to compensate for
language limits (no first-class decoration, type erasure), and twenty years
of backward compatibility that forbids subtraction. Go's first-class
functions make decoration explicit nesting; config-key activation makes
"when does this wire" a non-question. Where a family needs to plug in, it
gets a typed seam (`.Export(gs.As[...])` collection, a Driver registry) — a
contract for that family, not a hook that can rewrite any bean.

- **Runtime scanning (Spring Boot classpath scanning) rejected.** All bean
  metadata is registered by `init()`, so there is no classpath walk. Cost:
  every bean provider package must be imported — directly or transitively —
  by `main`, otherwise its `init()` never runs and its beans are silently
  absent. Benefit: predictable boot, no ordering surprises,
  zero reflection after wiring.
- **`autoconfig.exclude` rejected.** Spring Boot needs an exclusion list
  because auto-configurations activate on classpath presence — eager and
  implicit, so an off-switch had to exist somewhere else. Here every starter
  registers through a config-key condition (`gs.Module(gs.OnProperty
  ("spring.X"), ...)`): importing a starter wires nothing until its
  configuration exists, so "disable it" is simply "don't configure it", and
  flag-gated conveniences (e.g. `spring.pprof.enabled`) carry their own
  explicit switch. Cost: a starter author must gate registration on a
  config key — no unconditional `init()` providers. Benefit: one switch, in
  the configuration, where the operator already looks — no second, drifting
  off-switch to keep in sync.
- **`@Primary` rejected.** Injection is name-first (`autowire:"name"`), so
  the by-type ambiguity `@Primary` resolves in Spring mostly cannot arise;
  when a default bean must yield to a user-provided one, that is expressed
  as a condition on the default's *registration* — `gs.OnMissingBean[T]()`,
  `gs.OnSingleBean[T]()`, `gs.OnProperty(...)` — deciding whether the bean
  exists at all rather than which existing bean wins. Cost: a starter that
  ships a default must gate it with an explicit condition instead of relying
  on a priority mark. Benefit: no hidden priority layer in the type index —
  the graph stays name-addressed and the whole decision is visible at
  registration time.
- **Compile-time DI (Wire-style codegen) rejected.** The container keeps a
  runtime graph so conditional modules, `Group` from configuration maps,
  and hot-refresh of `Dync[T]` can decide at boot / at runtime what to
  materialize. Reflection is confined to the boot pass.
- **Implicit interface indexing rejected.** Indexing every implemented
  interface would make `OnBean[Iface]` and `[]Iface autowire:""` non-local
  and hard to reason about. Requiring explicit `Export` keeps the type
  index a closed set the maintainer controls.
