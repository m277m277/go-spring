# httputil

[English](README.md) | [中文](README_CN.md)

`httputil` 从入站 HTTP 请求派生 OpenTelemetry HTTP 语义约定的属性**值**，使各 server starter（gin、echo、fiber……）无需各自重复实现相同映射。函数返回纯 Go 类型（string/int），不是 `attribute.KeyValue`，因此本包不依赖任何 OTel 组件——调用方按需用 `attribute.String`/`attribute.Int` 包装。遵循的约定是自 v1.27.0 起稳定的 OTel HTTP semconv：`url.scheme`、`network.protocol.version`、`server.address`、`server.port`。

## 使用方式

导入路径：`go-spring.org/stdlib/httputil`。

```go
import (
    "go-spring.org/stdlib/httputil"
    "go.opentelemetry.io/otel/attribute"
)

r := c.Request
scheme := httputil.Scheme(r)                          // "https" / "http"
proto := httputil.ProtocolVersion(r.Proto)            // "1.1" / "2" / "3"
addr, port := httputil.ServerAddrPort(r.Host, scheme) // host, 默认端口时为 0

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

| 函数 | 返回 | Semconv 属性 |
|---|---|---|
| `Scheme(r *http.Request) string` | `"https"`（TLS）/ `"http"` | `url.scheme` |
| `ProtocolVersion(proto string) string` | `"1.0"`/`"1.1"`/`"2"`/`"3"` | `network.protocol.version` |
| `ServerAddrPort(host, scheme string) (string, int)` | host、port（默认端口时 0） | `server.address`、`server.port` |
| `FlattenHeader(h http.Header) string` | `"K: V; K: V"` |（日志便捷工具，非 semconv 属性）|

## 关键设计

- **边界**：只有每个 server starter 都会重复的 semconv 值派生才放在这里。它不是请求解析、路由或中间件库。
- **只产出值、不碰 OTel 类型**：函数计算字符串/整数。未引入 OTel 的 starter 也能用；引入了的用 `attribute.String`/`attribute.Int` 包装。semconv 的*键名*留在各 starter（属 OTel 关注点），只共享*值派生*。
- **尽量解耦 `*http.Request`**：`ProtocolVersion` 和 `ServerAddrPort` 接收纯字符串而非请求对象，使非 gin 传输（gRPC、经 quic 的 HTTP/3）从别处拿到 proto/host 时也能复用。`Scheme` 接收请求是因为要读 `r.TLS`。
- **semconv v1.27.0 规则**：端口缺省或为 scheme 默认端口（80/443）时 `ServerAddrPort` 返回 0，因约定仅在非默认端口时才要求 `server.port` 字段。
- **依赖**：仅标准库（`net`、`net/http`、`strconv`、`strings`），无 OTel 依赖。

## License

Apache License 2.0
