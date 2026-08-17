# typeutil
[English](README.md) | [中文](README_CN.md)

`typeutil` 提供 Go-Spring 容器在扫描 autowire / provider 目标时用来判定类型的反射工具集——bean 还是属性、是不是构造器签名、基本类型还是结构体等。集中收敛这些判定，让容器错误信息里出现的术语在同一份文件里可 grep。属于零依赖的 `stdlib` 层。

## 使用方式

```go
import (
    "reflect"
    "go-spring.org/stdlib/typeutil"
)

if typeutil.IsConstructor(reflect.TypeOf(fn)) {
    // 可以按构造器注册
}
```

### API

类型约束：

- `IntType`、`UintType`、`FloatType` —— 对应 Go 数值族的泛型约束。

`reflect.Type` 上的判定函数：

- `IsFuncType(t)` —— 是否函数类型。
- `IsErrorType(t)` —— 是不是 `error` 或实现了 `error`。
- `ReturnNothing(t)` —— 函数无返回值。
- `ReturnOnlyError(t)` —— 函数只返回一个 error。
- `IsConstructor(t)` —— 单返回值非 error，或双返回值第二个是 error。
- `IsPrimitiveValueType(t)` —— int / uint / float / string / bool。
- `IsPropBindingTarget(t)` —— 属性绑定的合法目标（基本类型、结构体、或元素为上述类型的集合）。
- `IsBeanType(t)` —— chan、func、interface、或 `*struct`。
- `IsBeanInjectionTarget(t)` —— bean 类型，或元素为 bean 的集合。

## 关键设计

- **拥有术语定义，不是反射库。** "基本值类型"、"构造器"、"bean 类型"、"注入 / 绑定目标"都定义在同一份文件里；需要触碰运行时值（而非 `reflect.Type`）的逻辑放在使用它的代码旁边。
- **`IsBeanType` 形态**：`chan`、`func`、`interface`、`*struct`。值类型的 struct 被有意排除——容器面向引用工作，才能注入代理 / 切面；需要值语义的调用点应显式取指针。
- **`IsConstructor` 形态**：要么 `func() T`（T 不是 error），要么 `func() (T, error)`。其它形态（多返回值、单 `func() error`）会在容器上层被拒。
- **绑定判定分开两个。** `IsPropBindingTarget` 与 `IsBeanInjectionTarget` 各自独立："给我一个配置值"和"给我一个依赖"是两条注入路径，合法目标形态不同。
- **nil 守卫只做了一半。** nil `reflect.Type` 只在 `IsErrorType` 和 `IsBeanInjectionTarget` 返回 `false`，其它函数没有守卫，可能为 nil 的调用点需自己先判。
- **零依赖。** 除 `reflect` 和泛型约束外别无他物；spring 容器核心内部（bean、arg、injecting、配置绑定）都引用这个包，添加新依赖的门槛非常高。

## License

Apache License 2.0
