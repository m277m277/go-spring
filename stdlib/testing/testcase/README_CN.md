# testcase

[English](README.md) | [中文](README_CN.md)

`testcase` 是把 `stdlib/testing/internal` 里的检查通过 [assert](../assert/) **与** [require](../require/) 两个入口都跑一遍的共享测试套。这是纯测试包（`package testcase_test`），不导出任何代码；它存在的意义是防止两个入口的行为漂移。要写断言请直接用 `assert` / `require`。

## 使用方式

没有对外可 import 的 API——套件只被 `go test` 发现：

```
go test ./stdlib/testing/...
```

六个文件，每族断言一份：

| 文件 | 覆盖 |
|------|------|
| `assert_test.go` | 泛型 `That` 与 `Panic` |
| `error_test.go`  | `Error`（`Is` / `Matches` / `String` ...） |
| `number_test.go` | `Number[T]` |
| `string_test.go` | `String` |
| `slice_test.go`  | `Slice[T]` |
| `map_test.go`    | `Map[K,V]` |

## 关键设计

把同一批场景经两个入口都跑一遍，保证它们有同样的方法与同样的签名、给出一致格式的失败信息，唯一差异只是"测试是否立即停止"。

- **伪 `TestingT`。** 套件用 `internal.MockTestingT` 驱动断言，它把失败输出记录进缓冲而不真让外层 test 失败，因此能对失败信息的**内容**做断言。
- **每族一个测试文件**，与 `internal/*.go` 拆分对齐：某族变更就对应一个测试文件。
- **无导出符号、不写生产代码。** 目录里只有 `*_test.go` 文件，扫模块公开 API 的工具在这里看不到东西。套件只依赖 Go 标准库以及 `testing/internal` 和两个包装包——引第三方会经构建期耦合漏进包装包。
- **共享套件优于复制；纯测试包，不做 helper 库。** 在 `assert` 与 `require` 里各写一份测试，两个模式各自维护必然漂移，同一套用两个入口跑强制对齐。而把伪 `TestingT` 与场景表公开会诱导使用方嵌进自己的测试，两模式契约不是给外部这么用的。

## 许可证

Apache License 2.0
