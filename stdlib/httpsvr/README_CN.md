# httpsvr

[English](README.md) | [中文](README_CN.md)

`httpsvr` 是极薄的 HTTP 服务端工具包：基于 Go 1.22+ `ServeMux` 的 `Server` 缝隙、`RequestContext` 抽象、以及 JSON / SSE 泛型 handler。它是 `stdlib/httpclt` 的服务端对偶——生成 handler 所插入的服务端骨架——同样位于 `stdlib` 零依赖层：仅用 `net/http` 与仓库内 `jsonflow` / `ctxcache` / `errutil`。

## 使用方式

导入路径：`go-spring.org/stdlib/httpsvr`。

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

API 概览：

- `Server` 接口 + `SimpleServer`——基于 `http.ServeMux`，支持方法级 pattern（`"GET /users/{id}"`）。
- `RequestContext` 接口 + `SimpleContext`——统一 `*http.Request` / `http.ResponseWriter` / `PathValue` 访问，可经 `WithRequestContext` / `GetRequestContext` 存 / 取 `context.Context`。
- `RequestObject`（`Bind` + `Validate`）与 `ReadRequest`——按 `Content-Type` 选 JSON 或 form 解码，不识别时用首字节嗅探，仅对 POST/PUT/PATCH 解析 body。
- `HandleJSON[Req, Resp]`——泛型 JSON handler 包装，写 `application/json` 并初始化 `ctxcache`。
- `HandleStream[Req, Resp]` + `Event[T]`——SSE，支持 `id` / `event` / `retry` 字段。
- 可覆写的 `ErrorHandler` 与 `ReadBody`（默认上限 10 MiB）。

## 关键设计

- **边界**：提供路由缝隙（`Server.Route`），starter 可在其上换任意底层 router 实现；提供 JSON / SSE 泛型 handler 包装，让 Go 1.22+ 生成 handler 只是薄适配器，而不重写解析/校验/编帧。拒绝成为完整 web 框架：无中间件链、无绑定 tag 魔法、无 DI——这些属于 starter 层或用户代码。
- **缝隙**：`Server` 接口只有一个方法 `Route(Router)`——starter 想换 router（chi / gin……）实现这一个即可，其他不动。`RequestContext` 是请求/响应对，通过 `WithRequestContext` 存进 `context.Context`，即使 handler 只拿到 `ctx`（经 `ctxcache` 等）也能取回 writer。`ReadBody` 与 `ErrorHandler` 刻意可变，应用可下调 body 上限或改用 JSON 错误体格式，而无需包装 `HandleJSON`。
- **body 解析规则**：只对 `POST` / `PUT` / `PATCH` 读 body；其他方法跳过 `decodeBody`，带 body 的 `GET` 视作无 body。`ReadRequest` 按 `Content-Type` 选 JSON / form，不识别时用首字节嗅探，让漏设 header 的 body 也能解析。`RequestObject.Bind` 在 body 解码之后运行；解码失败直接短路、不会调用 `Bind`，故对带 body 的方法 `Bind` 可假定字段已解码填充。
- **响应编帧**：JSON 路径在 handler 执行前就设 `Content-Type: application/json`，业务 handler 不会忘设。`HandleStream` 要求 `http.ResponseWriter` 实现 `http.Flusher`，否则经 `ErrorHandler` 报 500——包装 writer 时不能丢失 Flusher。
- **被否决方案**：不做自定义 router——Go 1.22 的 `http.ServeMux` 已支持方法级 pattern，足以承担本包的缝隙职责，引第三方 router 会破坏零依赖约定。不内置中间件切片——链式装配属于更高层（`cloud/experimental/security` 的中间件、各家族自带的方法级装饰器，或 starter 包装 `Server.Route` 缝隙），在这里内置会锁死顺序。JSON / form 两条编码路径 + 首字节嗅探——更完整的内容协商延后：真实 API 要么 JSON 要么 `x-www-form-urlencoded`，嗅探覆盖漏设 header 的常见场景。

## License

Apache License 2.0
