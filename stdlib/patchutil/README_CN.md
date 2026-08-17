# patchutil
[English](README.md) | [中文](README_CN.md)

`patchutil` 提供一个反射工具，用于清掉 `reflect.Value` 内部的 read-only
标记，从而给未导出字段赋值。它为框架内部无法修改目标类型的接缝处而生，
**仅供框架内部工具与测试使用**。属于 Go-Spring 零依赖的 `stdlib` 层。

## 使用方式

```go
import (
    "reflect"
    "go-spring.org/stdlib/patchutil"
)

f := patchutil.PatchValue(reflect.ValueOf(&obj).Elem().FieldByName("secret"))
f.SetString("new value")
```

### API

- `PatchValue(v reflect.Value) reflect.Value` —— 返回同一个 `Value`，
  但已清掉 `flagRO` 位，之后 `Set` 即便原本指向未导出字段也能成功。

## 关键设计

这个包从存在的第一天起，就是在"没有它更糟"与"请不要滥用"之间做取舍。

- **只做一个 unsafe 原语。** 仅清 RO 位——不是反射库，也不是通用
  "什么都能改"工具；可寻址性、Kind 等前提仍需调用方自己保证。其它
  反射工具在 `stdlib/typeutil` 或标准 `reflect` 中。
- **依赖内存布局。** 用 `unsafe` 触碰 `reflect.Value` 的精确布局与私有
  `flagStickyRO` / `flagEmbedRO` 位值——实践中跨 Go 版本稳定，但不在
  Go 1 兼容承诺范围内。`PatchValue` 直接修改其收到的 `Value` 的 flag
  字并返回同一个 `Value`；没有任何锁，谈不上线程安全。生产业务代码
  请**不要**使用。
- **它为什么存在。** 把这个包完全避掉的办法是让调用点导出必要字段；
  它之所以保留，是因为框架内部接缝（容器的注入代码）有时无法修改
  目标结构体。这类场景范围小、可审计，因此文件只有 40 行左右。

## License

Apache License 2.0
