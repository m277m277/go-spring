# assert
[English](README.md) | [中文](README_CN.md)

`assert` 提供 fluent、按类型分族的断言，失败**不停测试**——后续断言继续执行，一次测试可以报出多个问题。姊妹包 [`require`](../require/) 语义一致但首次失败即停。

完整断言参考与 `assert` / `require` 对比，见父包 [`testing`](../)。

## 使用方式

```go
package myapp_test

import (
    "testing"

    "go-spring.org/stdlib/testing/assert"
)

func TestUser(t *testing.T) {
    assert.That(t, "hello").Equal("hello")
    assert.Number(t, 42).GreaterThan(40)
    assert.String(t, "user@example.com").IsEmail()
    assert.Slice(t, []int{1, 2, 3}).Contains(2)

    // 这里失败不会停测——下一条断言依然会执行。
    assert.That(t, "a").Equal("b")
    assert.That(t, "c").Equal("c") // 仍然会执行
}
```

### 特性

- 泛型入口：`That` / `Error` / `Number[T]` / `String` / `Slice[T]` / `Map[K,V]` / 顶层 `Panic`。
- Fluent 链式检查（如 `.Equal(...)`、`.NotNil()`、`.Contains(...)`）。
- 所有方法末尾接受 `msg ...string` 传自定义失败信息。
- 零第三方依赖。

## 关键设计

`assert` 是 `stdlib/testing/internal` 共享断言引擎的**薄包装**，把"失败继续"模式做成 import 期的编译期选择，而不是每次调用传一个运行期 flag。

- **一个常量决定模式。** `const fatalOnFailure = false` 是让这个包变成 "assert" 而不是 "require" 的唯一一行；每个入口函数把它透传，失败检查走 `t.Error`（继续）而不是 `t.Fatal`（停止）。
- **不写自己的断言逻辑。** 所有检查在 `internal`，与 `require` 共享；入口返回的 fluent 值对象（`*internal.Assertion`、`*internal.ErrorAssertion` 等）两种模式用的是同一批类型，行为不可能漂移。
- **`internal.TestingT` 缝隙。** 每个入口接的 `*testing.T` 最小接口（`Helper` / `Error` / `Fatal`）——同一套断言能跑在真测试、subtest、以及 `testcase` 套件用来自省失败信息的 `MockTestingT` harness 里。
- **约束。** 除 `internal` 外零依赖（新加依赖会漏进每个模块的测试二进制）；与 `require` 的行为对等由 `testcase` 套件强制——同一批场景两个入口都跑。`Panic` 是顶层函数而非 fluent 链，因为目标是回调，不是值。
- **被否决的方案。** 一个包 + 运行期模式 flag 会让测试在一个函数里意外混模式；按 import 拆开让每个调用点的选择可见。依赖 testify 也被否决——API 有意接近以保留肌肉记忆，实现只用标准库。

## License

Apache License 2.0
