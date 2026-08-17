# require
[English](README.md) | [中文](README_CN.md)

`require` 提供 fluent、按类型分族的断言，**首次失败即停**——后续断言不再执行。姊妹包 [`assert`](../assert/) 语义一致但失败继续。完整断言参考与两者对比见父包 [`testing`](../)。

## 何时选 `require` 而不是 `assert`

当一次失败会让后面的检查全部无意义时用 `require`——如被 unwrap 的值是 nil，或 fixture 没装配成功。继续下去要么 panic，要么打出胡言乱语的错误。多个独立断言各自有意义时用 `assert`。

## 使用方式

```go
package myapp_test

import (
    "testing"

    "go-spring.org/stdlib/testing/assert"
    "go-spring.org/stdlib/testing/require"
)

func TestUser(t *testing.T) {
    user := loadUser()
    require.That(t, user).NotNil() // nil 就停——不然下一行 panic

    assert.String(t, user.Email).IsEmail()
    assert.Number(t, user.Age).GreaterThan(0)
}
```

### 特性

- 与 `assert` 完全同款 fluent API：`That` / `Error` / `Number[T]` / `String` / `Slice[T]` / `Map[K,V]` / 顶层 `Panic`。
- 失败走 `t.Fatal`——测试立即停止。
- 所有方法末尾接受 `msg ...string` 自定义失败信息。
- 零第三方依赖。

## 关键设计

`require` 是 `stdlib/testing/internal` 共享断言引擎的**薄包装**，与 [`assert`](../assert/) 唯一差异是失败检查走 `t.Fatal` 而非 `t.Error`。它存在的意义是把 fail-fast 的选择做到 import 期可见。

- **一个常量决定模式。** `const fatalOnFailure = true` 是让这个包变成 "require" 而不是 "assert" 的唯一一行；每个入口函数透传它。
- **不写自己的断言逻辑。** 检查在 `internal`，与 `assert` 共享；fluent 值对象（`*internal.Assertion` 等）与 `assert` 完全同款——签名上两个模式互为替换。
- **`internal.TestingT` 缝隙。** 每个入口接的 `*testing.T` 最小接口（`Helper` / `Error` / `Fatal`）。
- **约束。** 除 `internal` 外零依赖；与 `assert` 的行为对等由 `testcase` 套件强制——同一批场景两个入口都跑。`t.Fatal` 只停当前 test：通过 `require` 失败的 `t.Run` subtest 停掉自己，父 test 自己决定继不继续。
- **被否决的方案。** 运行期模式 flag 被否决——读代码时还得回溯是否有 `SetMode(...)` 把行为翻过；依赖 testify 也被否决——API 表面有意接近保留肌肉记忆，实现只用标准库。

## License

Apache License 2.0
