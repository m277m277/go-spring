# ctxcache

[English](README.md) | [中文](README_CN.md)

`ctxcache` 是一个强类型的、上下文作用域的缓存包，专为请求范围（request-scoped）的数据设计，属于零依赖的 `stdlib` 层。它把并发安全、写一次（write-once）的键值存储附加到 `context.Context` 上，让值在调用边界之间隐式传播，而无需污染函数签名。它不是通用缓存——没有 TTL、淘汰、命中率——只是一个作用域受控、类型明确的小型存储，生命周期恰好等于外层请求。典型用途：认证用户对象、权限列表、链路追踪元数据、调用链中共享的计算中间结果。

## 使用方式

在请求边界（如 HTTP 中间件）初始化缓存，退出时清理：

```go
ctx, cancel := ctxcache.Init(ctx)
defer cancel()
```

设置值。每个键只能设置一次，重复设置返回 `ErrKeyAlreadySet`：

```go
err := ctxcache.Set(ctx, "user", userInfo)
if err != nil {
    // 处理错误
}
```

在下游读取。`Get` 是泛型函数，类型参数正是类型安全的来源：

```go
value, err := ctxcache.Get[UserType](ctx, "user")
if err != nil {
    // 处理错误
}
```

调用 `Init` 返回的取消函数会清除所有缓存值，并使缓存永久不可用——之后的 `Get` 或 `Set` 都返回 `ErrCacheAlreadyCleared`：

```go
cancel()
```

### API

- `Init(ctx) (context.Context, func())` —— 附加缓存并返回其取消函数。幂等：
  对同一上下文重复调用返回原始上下文和无操作的取消函数。
- `Set[T](ctx, key, value) error` —— 为键赋值，每个键一次。
- `Get[T](ctx, key) (T, error)` —— 按带类型的键取值。
- `Cache.Clear()` —— 清空全部值并把缓存标记为已清理（幂等）。

包内返回的错误（均可用 `errors.Is` 判定）：

- `ErrCacheNotInitialized`：缓存未初始化
- `ErrCacheAlreadyCleared`：缓存已被清除
- `ErrKeyNotSet`：键未设置
- `ErrKeyAlreadySet`：键已被设置

完整示例：

```go
package main

import (
    "context"
    "fmt"
    "go-spring.org/stdlib/ctxcache"
)

type User struct {
    ID   int
    Name string
}

func main() {
    ctx := context.Background()

    // 初始化缓存
    ctx, cancel := ctxcache.Init(ctx)
    defer cancel()

    // 设置用户信息
    user := User{ID: 1, Name: "Alice"}
    if err := ctxcache.Set(ctx, "user", user); err != nil {
        panic(err)
    }

    // 在下游代码中获取用户信息
    retrievedUser, err := ctxcache.Get[User](ctx, "user")
    if err != nil {
        panic(err)
    }

    fmt.Printf("User: %+v\n", retrievedUser)
}
```

## 关键设计

这个包只承担一个模式："一次请求、一袋带类型的值、在边界清理"。所有数据
挂在具体的 context 上，没有进程级全局 map。

- **`Cache`**：通过私有 key 挂在 context 上的受互斥锁保护的
  `map[any]any`。每个 context 只有一个 `Cache`；对同一 context 再调
  `Init` 是幂等操作。
- **`TypedKey[T]`**：key 是 `(string, 类型)` 对，由泛型生成。名字相同但
  `T` 不同就是两个互不相干的槽位——这也是 `Get`/`Set` 必须是泛型而不是
  `any` 的原因。
- **显式生命周期**：`Init` 返回的取消函数会清空 map 并把缓存永久标记为
  已清理。有意不做"第二生命"——复用会静默地在请求间共享状态。
- **Key 写一次**，使"存在的值不会被调用方眼皮底下改掉"成为总契约。需要
  可变性的调用方应存指针，或用稳定 key 存 `sync.Map`。
- **取消函数必须调用**，通常在中间件边界 `defer`。漏调只会泄漏一个小
  map，不会泄漏 goroutine。
- **并发安全**：单互斥锁是最简单正确的选择；请求作用域数据大多顺序流转，
  锁竞争极少。

## License

`ctxcache` 基于 Apache License 2.0 发布，详见 [LICENSE](../../LICENSE)。
