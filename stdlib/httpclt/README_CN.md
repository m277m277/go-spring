# httpclt

[English](README.md) | [中文](README_CN.md)

`httpclt` 是生成的声明式 HTTP 客户端（见 `go-spring.org/cloud/experimental/httpx` 与代码生成器 `go-spring.org/gs-http-gen`）的运行时。本身无状态：承载 `Metadata`、应用 `RequestOption`、经 `stdlib/jsonflow` 编码 body，最终经调用方传入的 `*http.Client` 发出请求（未设置则回落到 `http.DefaultClient`）。它位于 `stdlib`，只 import `net/http`、`stdlib/jsonflow` 与少量标准库，让生成客户端保持 stdlib-only，不把 starter 依赖泄给调用方。

## 使用方式

导入路径：`go-spring.org/stdlib/httpclt`。生成客户端直接调用这些 helper；手写调用示例：

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
    Client:  client, // httpx 装配得到的 *http.Client
}
resp, out, err := httpclt.JSONResponse[*GreetResp](context.Background(), meta)
if err != nil {
    return err
}
defer resp.Body.Close()
fmt.Println(out.Message)
```

API 概览：

- `Metadata`——target/schema/method/pattern/query/body/header/config，加 `Client` 字段，让生成客户端可携带自己已埋点的传输层。
- `RequestOption` 通过 `CombineMetadata` 组合，内置 `WithHeader`、`WithConfig`。
- 流式 JSON 解码：`ObjectResponse[T]`（T 实现 `DecodeJSON`）与 `JSONResponse[T]`（泛型 `any`）。
- `QueryStringer` 是 query 编码扩展点；body 若实现 `EncodeForm` 则以表单发送，否则流式 JSON。
- `DoRequest` 是 `var`，测试/埋点可整段替换真正的 HTTP 调用。

## 关键设计

- **边界**：`httpclt` 推动 `Metadata` 走完请求构建、发送与流式解码，仅此而已。拒绝关心服务发现、负载均衡、resilience、trace 透传——这些都由注入到 `Metadata.Client` 的 `*http.Client` 负责（见 `go-spring.org/cloud/experimental/httpx`）。
- **缝隙**：`Metadata.Client` 是生成客户端接入 discovery + 负载均衡 + resilience + otel 的唯一缝隙，生成代码无需改动；为 nil 时静默回落 `http.DefaultClient`——生成器输出的这个字段，应用在测试或简单脚本里可能不填。包级 `DoRequest` 变量是测试/观测缝隙，替换它可不动生成的调用点、整段短路 HTTP 调用。`QueryStringer` / `EncodeForm` / `ResponseObject` 是两个编码可插拔点（`QueryForm`、`EncodeForm`）与一个解码可插拔点（`DecodeJSON`），由生成类型实现，运行时不对业务 struct 做运行时反射。
- **与生成器的契约**：`Metadata` 上的字段是与 `gs-http-gen` 输出的契约，改字段名即破坏契约。部分字段仅为契约保留：`Pattern` 由生成器输出但运行时从不读取，运行时只用 `RawPath`。默认流式 JSON：`ObjectResponse` / `JSONResponse` 经 `jsonflow` 增量解码，不在内存中缓冲整个响应体。
- **不在 httpclt 内构造 client（被否决方案）**：传输层、超时、Cookie jar、TLS 配置都在调用方注入的 `*http.Client` 里，这正是 client 侧集成（`starter-http-client`、测试、contract 桩）可插拔而不改生成代码的原因。对称地，不对业务类型做反射：接口方法（`QueryForm` / `EncodeForm` / `DecodeJSON`）让运行时保持快 + 零依赖，代价是让代码生成器承担对偶职责。

## License

Apache License 2.0
