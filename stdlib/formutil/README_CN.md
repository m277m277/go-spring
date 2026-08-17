# formutil
[English](README.md) | [中文](README_CN.md)

`formutil` 提供 Go 值与表单键值（`url.Values`、`[]string`）之间的泛型编解码
工具，供 Go-Spring 的 HTTP 客户端 / 服务端绑定代码使用。属于零依赖的
`stdlib` 层；本包不做校验——唯一的横向规则是范围检查，其它都交由上层。

## 使用方式

```go
import (
    "net/url"
    "go-spring.org/stdlib/formutil"
)

// 解码单值
n, err := formutil.DecodeInt[int]("page", []string{"3"})

// 解码重复字段
ids, err := formutil.DecodeList("ids",
    []string{"1", "2", "3"}, formutil.DecodeInt[int64])

// 编码到 url.Values
v := url.Values{}
_ = formutil.EncodeString(v, "name", "alice")
_ = formutil.EncodeIntPtr[int64](v, "opt", nil) // 缺省
```

### API

- 对 `bool`、有 / 无符号整数、浮点数、`string`、字节切片以及任意 JSON 提供
  对称的 `Decode<Type>` / `Encode<Type>` 对。
- `<Type>Ptr` 变体（`Bytes` / `JSON` / `List` 除外）：编码时 `nil` 表示
  "字段缺省"，解码时返回 `*T`。
- 通过泛型 `DecodeList` / `EncodeList` 处理重复字段。
- 整数 / 浮点数解码借助 `stdlib/mathutil` 做溢出检查。
- JSON 编解码委托给 `stdlib/jsonflow`。

### 规则

- 所有非列表 Decoder 遇到多个原始值时会报错（"too many values for form
  field ..."），遇到空值列表时同样报错（"missing value for form field ..."）。
- 整数 / 无符号 / 浮点解码在结果不适合目标 `T` 时返回溢出错误。
- `DecodeBytes` / `EncodeBytes` 使用标准 base64。
- `EncodeXxxPtr` 在指针为 `nil` 时完全省略该字段。

## 关键设计

只提供单字段、原始类型级别的编解码，结构体绑定交给调用方（HTTP 框架的
binder / 声明式 client）；编码与解码对称，写字段的代码能读回同一字段。

- 泛型函数集（`Decode/EncodeInt[T]`）而非每类型一个文件：接口扁平，代码
  生成器可按字段类型对应到一个函数。
- 解码入参统一 `[]string`，与 `url.Values[key]` 对齐；编码侧 `nil` 指针表示
  缺省，绑定器无需位图就能区分"未设置"与"零值"。
- 字节切片用 base64、JSON 委托 `stdlib/jsonflow`，锁死规范表达，使同一
  binder 的两端天生对齐。
- 约束：除 `stdlib` 内部包外零依赖；浮点数无论类型宽窄都按
  `strconv.FormatFloat(..., 'f', -1, 64)` 格式化，`T = float32` 时丢失精度
  信息；溢出错误是 `errutil.Explain` 的普通错误串而非 sentinel。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
