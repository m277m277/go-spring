# httputil

[English](README.md) | [中文](README_CN.md)

`httputil` derives OpenTelemetry HTTP semantic-convention attribute *values*
from an inbound HTTP request, so server starters (gin, echo, fiber, ...) don't
each reimplement the same mappings. The functions return plain Go types
(string/int), not `attribute.KeyValue`, so the package stays free of any OTel
dependency — callers wrap the values with `attribute.String`/`attribute.Int`.
The conventions followed are the OTel HTTP semconv stable since v1.27.0:
`url.scheme`, `network.protocol.version`, `server.address`, `server.port`.

## Usage

Import path: `go-spring.org/stdlib/httputil`.

```go
import (
    "go-spring.org/stdlib/httputil"
    "go.opentelemetry.io/otel/attribute"
)

r := c.Request
scheme := httputil.Scheme(r)                          // "https" / "http"
proto := httputil.ProtocolVersion(r.Proto)            // "1.1" / "2" / "3"
addr, port := httputil.ServerAddrPort(r.Host, scheme) // host, 0 if default port

attrs := []attribute.KeyValue{
    attribute.String("url.scheme", scheme),
    attribute.String("network.protocol.version", proto),
    attribute.String("server.address", addr),
}
if port != 0 {
    attrs = append(attrs, attribute.Int("server.port", port))
}
```

### API

| Function | Returns | Semconv attribute |
|---|---|---|
| `Scheme(r *http.Request) string` | `"https"` (TLS) / `"http"` | `url.scheme` |
| `ProtocolVersion(proto string) string` | `"1.0"`/`"1.1"`/`"2"`/`"3"` | `network.protocol.version` |
| `ServerAddrPort(host, scheme string) (string, int)` | host, port (0 if default) | `server.address`, `server.port` |
| `FlattenHeader(h http.Header) string` | `"K: V; K: V"` | (log convenience, not a semconv attribute) |

## Design

- **Boundaries**: only the semconv-value derivations every server starter
  duplicates belong here; not a request-parsing, routing, or middleware library.
- **Value-only, no OTel types**: the functions compute strings/ints. A starter
  that hasn't imported OTel can still use them; one that has wraps them with
  `attribute.String`/`attribute.Int`. The semconv *key names* stay in each
  starter (they're an OTel concern); only the *value derivation* is shared.
- **Decoupled from `*http.Request` where possible**: `ProtocolVersion` and
  `ServerAddrPort` take plain strings, not a request, so non-gin transports
  (gRPC, HTTP/3 via quic) that obtain the proto or host elsewhere can reuse
  them. `Scheme` takes the request because it reads `r.TLS`.
- **semconv v1.27.0 rule**: `ServerAddrPort` returns 0 when the port is absent
  or the scheme default (80/443) — the convention requires `server.port` only
  when non-default.
- **Dependencies**: standard library only (`net`, `net/http`, `strconv`, `strings`).

## License

Apache License 2.0
