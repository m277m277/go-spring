# ordered
[English](README.md) | [中文](README_CN.md)

`ordered` 目前只提供一个产生 map 稳定遍历顺序的工具。属于 Go-Spring 零依赖
的 `stdlib` 层；这个包是未来"稳定遍历顺序"类工具的命名归口，不是有序 map
容器——当前场景用原生 map 加这个 helper 就够了。

## 使用方式

```go
import "go-spring.org/stdlib/ordered"

for _, k := range ordered.MapKeys(m) {
    fmt.Println(k, m[k])
}
```

### API

- `MapKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K` —— 排序后的 key
  切片。

框架内凡是日志、JSON 序列化、诊断输出需要稳定 key 顺序的地方都会用它。

## 关键设计

- 一步以稳定顺序遍历 map，省去每个调用点的 `sort.Strings` 加临时切片。
- `cmp.Ordered` 约束（Go 1.21+）覆盖数值和字符串 key，无需为不同类型重复
  写函数；内部用 `slices.Sort` 而非 `sort.Strings`，保持泛型。
- 返回切片是独立拷贝，调用方可以随意修改。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
