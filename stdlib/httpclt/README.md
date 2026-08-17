# httpclt

[English](README.md) | [中文](README_CN.md)

`httpclt` is the runtime the generated declarative HTTP client (see
`go-spring.org/cloud/experimental/httpx` and the `go-spring.org/gs-http-gen`
code generator) calls into. It holds no state itself: it carries request
`Metadata`, applies `RequestOption`s, encodes the body via `stdlib/jsonflow`,
and dispatches the call through the caller-supplied `*http.Client` (falling
back to `http.DefaultClient` when none is set). It lives in `stdlib` and
imports only `net/http`, `stdlib/jsonflow` and a couple of standard packages,
so a generated client stays stdlib-only and never leaks a starter dependency
into caller code.

## Usage

Import path: `go-spring.org/stdlib/httpclt`. Generated clients call the helpers
directly; a hand-written call looks like:

```go
import (
    "context"
    "fmt"
    "net/http"

    "go-spring.org/stdlib/httpclt"
)

type GreetResp struct{ Message string }

meta := httpclt.Metadata{
    Target:  "user-svc",
    Schema:  "http",
    Method:  http.MethodGet,
    RawPath: "/greet",
    Header:  http.Header{"X-Trace": []string{"1"}},
    Client:  client, // an *http.Client whose Transport is built by httpx
}
resp, out, err := httpclt.JSONResponse[*GreetResp](context.Background(), meta)
if err != nil {
    return err
}
defer resp.Body.Close()
fmt.Println(out.Message)
```

API surface:

- `Metadata` — target/schema/method/pattern/query/body/header/config, plus a
  `Client` field so a generated client can carry its own instrumented transport.
- `RequestOption` composition via `CombineMetadata`, with `WithHeader` and
  `WithConfig` built in.
- Streaming JSON decode helpers: `ObjectResponse[T]` for a type that implements
  `DecodeJSON`, and `JSONResponse[T]` for a generic `any`.
- `QueryStringer` extension point for query encoding; bodies implementing
  `EncodeForm` are sent as form-encoded, otherwise streamed as JSON.
- `DoRequest` is a `var` so tests / instrumentation can stub the whole HTTP hop.

## Design

- **Boundaries**: `httpclt` drives the generator-emitted `Metadata` through
  request construction, dispatch and streaming decode — nothing else. It
  refuses to know about service discovery, load balancing, resilience or trace
  propagation: all of that belongs to the `*http.Client` injected via
  `Metadata.Client`.
- **Seams**: `Metadata.Client` is the single seam through which a generated
  client picks up discovery + load balancing + resilience + otel without
  changing any generated code. When nil it silently falls back to
  `http.DefaultClient`, because the generator emits a field the application
  may leave unset in tests or simple scripts. The package-level `DoRequest`
  variable is the test/observability seam: replacing it short-circuits the
  whole HTTP hop without touching the generated call site. `QueryStringer` /
  `EncodeForm` / `ResponseObject` are the two encoding pluggables (`QueryForm`,
  `EncodeForm`) and the one decoding pluggable (`DecodeJSON`) that generated
  types implement, so the runtime never reflects over business structs.
- **Generator contract**: fields on `Metadata` are the contract with
  `gs-http-gen` output — renaming a field breaks it. Some fields exist purely
  for that contract: `Pattern` is emitted by the generator but never read by
  the runtime, which only uses `RawPath`. Streaming JSON is the default:
  `ObjectResponse` / `JSONResponse` decode incrementally through `jsonflow`,
  so the runtime does not buffer the full response body in memory.
- **No client construction inside httpclt** (rejected alternative): transport,
  timeouts, cookie jar and TLS config all live in the injected `*http.Client`.
  This is what makes client-side integrations (`starter-http-client`, tests,
  contract stubs) pluggable without changing generated code. Symmetrically, no
  reflection over business types: interface methods (`QueryForm`, `EncodeForm`,
  `DecodeJSON`) keep the runtime fast and dependency-free, at the cost of the
  code generator carrying its weight on the other side.

## License

Apache License 2.0
