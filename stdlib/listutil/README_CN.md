# listutil

[English](README.md) | [中文](README_CN.md)

`listutil` 给 Go 的 `container/list` 加上泛型类型安全外壳，并补充一些切片、Writer 相关的便捷函数。属于零依赖的 `stdlib` 层：不是链表重写，也不是函数式集合库——每个方法都是内嵌 stdlib 类型上的一行转发，外加框架代码里高频出现的小工具。

## 使用方式

导入路径：`go-spring.org/stdlib/listutil`。

```go
import "go-spring.org/stdlib/listutil"

l := listutil.New[int]()
l.PushBack(1)
l.PushBack(2)

for e := l.Front(); e.Valid(); e = e.Next() {
    _ = e.Value() // int
}
```

API 概览：

- `List[T]` / `Element[T]`——对 `container/list.List` / `list.Element` 的泛型薄封装，保持双向链表 API，同时把 `any` 换成具体类型。
- 便捷函数：
  - `SliceOf[T](items ...T) []T`——用变参构造切片的语法糖。
  - `ListOf[T](items ...T) *list.List`——用变参构造 `*list.List`。
  - `AllOfList[T](l *list.List) []T`——把 list 元素收集为 `[]T`（元素类型不匹配会 panic）。
  - `WriteStrings(w io.Writer, values ...string) error`——依次写入字符串，首个错误即停止。

## 关键设计

- **补回编译期类型信息，而非重写数据结构**：每个方法都转发给内嵌的 `container/list` 类型，数据结构及其语义仍是标准库的。
- **`Element[T]` 通过指针嵌入 `*list.Element`**，`Valid()` 直接判空即可；`Element[T]` 的零值本身就是遍历结束的 "nil" 标记。
- **`AllOfList` 混类型时直接 panic**——有意为之：它走 `e.Value.(T)`，调用方混用类型即 panic。检查版本得引入 `ok, err` 形态，与多数调用点期望不符。
- **不校验外来链表元素**：该泛型封装**不会**检查传入的 `Element[T]` 是否来自别的链表——`container/list` 本身会 panic，封装层没有额外拦截。

## License

Apache License 2.0
