# mathutil
[English](README.md) | [中文](README_CN.md)

`mathutil` 提供泛型溢出检查，用于把 `int64` / `uint64` / `float64` 收敛到
更窄的数值类型时判断是否越界。属于 Go-Spring 零依赖的 `stdlib` 层，目前只
承担 JSON / 表单绑定需要的溢出检查——不是通用数值库，不做饱和转换；调用方
拿到 bool 后自行决定错误处理。

## 使用方式

```go
import "go-spring.org/stdlib/mathutil"

if mathutil.OverflowInt[int16](v) {
    return errors.New("out of range")
}
```

### API

- `OverflowInt[T ~int|~int8|~int16|~int32|~int64](v int64) bool`
- `OverflowUint[T ~uint|~uint8|~uint16|~uint32|~uint64](v uint64) bool`
- `OverflowFloat[T ~float32|~float64](v float64) bool`

值无法安全转换到 `T` 时返回 `true`。被 `stdlib/formutil` 与
`stdlib/jsonflow` 用于把数值解码到更窄目标类型时做范围校验。

## 关键设计

- 类型分派基于 `T` 零值的 `unsafe.Sizeof` 做 switch；编译期分派需要为每个
  类型引一个函数，反而把分支推到每个调用点——因为只在解码边界调用，当前
  形态可以接受。
- `OverflowInt[int64]` / `OverflowUint[uint64]` 是有意为之的空操作：调用方
  直接传 `strconv` 产出的值，"不需要截断"的分支必须便宜。
- `OverflowFloat[float64]` 恒返回 `false`；`OverflowFloat[float32]` 与
  `±math.MaxFloat32` 比较；次正规数和 NaN 不当作溢出。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
