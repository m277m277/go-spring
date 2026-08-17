# httpsvr

[English](README.md) | [中文](README_CN.md)

`httpsvr` is a thin HTTP server toolkit: a Go 1.22+ `ServeMux`-based `Server`
seam, a `RequestContext` abstraction, and generic handler wrappers for JSON and
Server-Sent Events. It is the server-side counterpart to `stdlib/httpclt` —
the shape that generated handlers plug into — and, like it, stays in `stdlib`'s
zero-dependency layer: everything is `net/http` plus the in-repo `jsonflow` /
`ctxcache` / `errutil` utilities.

## Usage

Import path: `go-spring.org/stdlib/httpsvr`.

```go
import (
    "context"
    "net/http"

    "go-spring.org/stdlib/httpsvr"
)

type GreetReq struct {
    Name string `form:"name"`
}
func (r *GreetReq) Bind(rq *http.Request) error { r.Name = rq.URL.Query().Get("name"); return nil }
func (r *GreetReq) Validate() error              { return nil }

type GreetResp struct{ Message string }

func main() {
    s := httpsvr.NewSimpleServer(":8080")
    s.Route(httpsvr.Router{
        Method:  http.MethodGet,
        Pattern: "/greet",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            httpsvr.HandleJSON(w, r, &GreetReq{}, func(ctx context.Context, req *GreetReq) *GreetResp {
                return &GreetResp{Message: "Hello, " + req.Name + "!"}
            })
        },
    })
    _ = s.ListenAndServe()
}
```

API surface:

- `Server` interface + `SimpleServer` — `http.ServeMux` with method-scoped
  patterns (`"GET /users/{id}"`).
- `RequestContext` interface + `SimpleContext` — unified `*http.Request` /
  `http.ResponseWriter` / `PathValue` access, stashable in `context.Context`
  via `WithRequestContext` / `GetRequestContext`.
- `RequestObject` (`Bind` + `Validate`) and `ReadRequest` — body parsing that
  picks JSON vs form by `Content-Type`, with sniff fallback, honouring
  `MethodPost/Put/Patch` only.
- `HandleJSON[Req, Resp]` — generic JSON handler wrapper, writes
  `application/json` and initializes `ctxcache`.
- `HandleStream[Req, Resp]` + `Event[T]` — Server-Sent Events with `id` /
  `event` / `retry` fields.
- Overridable `ErrorHandler` and `ReadBody` (default 10 MiB cap).

## Design

- **Boundaries**: provide a routing seam (`Server.Route`) that a starter can
  implement over any underlying router, and wrap generic JSON / SSE handlers so
  a generated Go 1.22+ handler is a small adapter rather than a re-implementation
  of parsing, validation and framing. It refuses to become a full web framework:
  no middleware chain, no binding-tag magic, no dependency injection — those
  belong at the starter layer or in user code.
- **Seams**: the `Server` interface is one method, `Route(Router)` — a starter
  wanting a different router (chi, gin, ...) implements it and everything else
  keeps working. `RequestContext` is the request/response pair, stored via
  `WithRequestContext` so a handler that received only a `context.Context`
  (through `ctxcache` or friends) can still reach the writer. `ReadBody` and
  `ErrorHandler` are deliberately mutable so an application can enforce a
  smaller body cap or a JSON error envelope without wrapping `HandleJSON`.
- **Body-parsing rules**: bodies are read only for `POST` / `PUT` / `PATCH`;
  any other method skips `decodeBody`, and a `GET`-with-body request is treated
  as bodyless. `ReadRequest` picks JSON vs form by `Content-Type`, falling back
  to a first-byte sniff so an unlabelled body still parses. `RequestObject.Bind`
  runs after body decode; a decode error short-circuits before `Bind`, so `Bind`
  may assume decoded fields are already populated for body-carrying methods.
- **Response framing**: the JSON path sets `Content-Type: application/json`
  before the handler runs, so a business handler cannot forget it. `HandleStream`
  requires the `http.ResponseWriter` to implement `http.Flusher` and reports a
  500 through `ErrorHandler` otherwise — wrapping writers must not hide
  flushing.
- **Rejected alternatives**: no custom router — Go 1.22 gave `http.ServeMux`
  method-aware patterns, which is enough for the seam-level responsibility of
  this package, and a third-party router would violate the zero-dependency
  rule. No middleware slice — chains belong at higher layers
  (the `cloud/experimental/security` middleware
  chain, or a starter wrapping the `Server.Route` seam); baking one in here
  would force users into that ordering. Two encoding paths (JSON / form) with a
  first-byte sniff — richer content negotiation is deferred: real APIs either
  use JSON or `x-www-form-urlencoded`, and the sniff covers the common case of
  a missing header.

## License

Apache License 2.0
