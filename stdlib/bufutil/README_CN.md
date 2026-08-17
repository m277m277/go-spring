# bufutil

[English](README.md) | [中文](README_CN.md)

`bufutil` 提供用于旁路复制的有界缓冲区，要求复制过程绝不反向压迫（backpressure）或报错给主流程。`LimitedBuffer` 是带字节上限的 `bytes.Buffer`：超过上限的写入被静默丢弃，但 `Write` 永远报告完整写入量并返回 nil 错误。凡是需要**在主消费者之外旁路复制一份流**的场合——丢了尾部可以接受，但拖慢或搞挂主流程不行——都可以用它：访问日志抓取请求/响应体（通过 `io.TeeReader` 镜像 body，`starter-gin` 就是这么用的）、流式/SSE 分块抓取（事件之间 `Reset()` 复用同一缓冲区）、以及任何截断无妨且绝不能成为故障点的调试/审计影子拷贝。需要完整数据、精确字节计数、流控反压或并发写入时**不要**用它。

## 使用方式

通过 `io.TeeReader` 抓取 body，且无内存耗尽风险：

```go
import (
    "io"
    "go-spring.org/stdlib/bufutil"
)

// 把请求体镜像到有界缓冲区供访问日志使用。handler 仍能读到全部字节；缓冲区最多保留 512 KiB。
capture := bufutil.NewLimitedBuffer(512 * 1024)
body = io.TeeReader(r.Body, capture)
// ... handler 读取 body ...
log.Printf("req.body=%s", capture.String())
```

流式响应可复用同一个缓冲区逐块抓取：

```go
buf := bufutil.NewLimitedBuffer(l.limit)
for each event {
    buf.Reset() // 保留上限，丢弃上一块
    io.Copy(buf, eventReader)
    log.Printf("event=%s", buf.String())
}
```

### API

| 方法 | 说明 |
|---|---|
| `NewLimitedBuffer(max int) *LimitedBuffer` | 创建最多保留 `max` 字节的缓冲区（`max < 0` 时 panic）。 |
| `Write(p []byte) (int, error)` | 追加至上限，丢弃溢出；永远返回 `len(p), nil`。 |
| `WriteString(s string) (int, error)` | `Write` 的字符串便捷封装。 |
| `Bytes() []byte` | 已缓冲字节（别名内部存储）。 |
| `String() string` | 已缓冲字节的字符串形式。 |
| `Len() int` / `Cap() int` | 当前大小 / 上限。 |
| `Reset()` | 清空内容（保留上限）以便复用。 |

## 关键设计

上限落在旁路副本的写入侧，这正是主流程不受影响的原因：

| 类型 | 限制侧 | 溢出行为 | 影响主流程？ |
|---|---|---|---|
| `bytes.Buffer` | 无 | 无限增长 | 否（但有内存风险） |
| `io.LimitReader` | 读取侧 | 主消费者读到截断的流 | **是** |
| `LimitedBuffer` | 写入侧 | 旁路副本被静默截断 | 否 |

- **静默溢出 + 谎报全量写入**：决定性特性。`Write` 丢弃超限字节却返回 `len(p), nil`。这正是抓取缓冲区满时 `io.TeeReader` 不被阻塞的关键——另一选择（报错或少写）会破坏主流程的读取。
- **`NewLimitedBuffer(max)` 构造、零值 = 上限 0**：零值 `LimitedBuffer` 丢弃一切，是安全的默认值；调用方需显式设置非零上限。负上限 panic（属编程错误，非运行时状况）。
- **`Bytes()` 别名内部存储**：继承自 `bytes.Buffer`；该切片会在下一次 `Write`/`Reset` 后失效。需要稳定副本的调用方须在下一次写入前取走。
- **设计上有损**：超过上限的数据直接丢失，不留丢弃了多少的记录。需要精确字节计数的调用方须自行追踪。
- **按字节边界截断，不按字符边界**：多字节 UTF-8 字符（中文、emoji）可能在尾部被劈开，`Bytes()`/`String()` 留下非法 UTF-8。伤害严格局部——UTF-8 自同步，截断点之前的内容不受影响——是否清理由消费方自选（`strings.ToValidUTF8`），不在每次抓取上内置全量扫描。
- **不可并发使用**，与 `bytes.Buffer` 一致。上限在构造时设定一次，永不增长；`Reset` 清空内容但保留上限。

## License

`bufutil` 基于 Apache License 2.0 发布，详见 [LICENSE](../../LICENSE)。
