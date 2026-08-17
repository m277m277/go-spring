# textstyle
[English](README.md) | [中文](README_CN.md)

`textstyle` 用 ANSI 转义序列为字符串加颜色和样式，用于终端输出。它永远输出转义码、不检测目标是不是终端，适合本身就知道该不该加样式的 CLI 工具与日志辅助逻辑。属于 Go-Spring 零依赖的 `stdlib` 层。

## 使用方式

```go
import "go-spring.org/stdlib/textstyle"

fmt.Println(textstyle.Red.Sprint("error: connection refused"))
fmt.Println(textstyle.NewText(textstyle.Bold, textstyle.Green).
    Sprintf("ok %d/%d", n, total))
```

### 功能

- 样式属性：`Bold`、`Italic`、`Underline`、`ReverseVideo`、`CrossedOut`。
- 前景色：`Black`、`Red`、`Green`、`Yellow`、`Blue`、`Magenta`、`Cyan`、`White`。
- 背景色：`BgBlack`、`BgRed`、`BgGreen`、`BgYellow`、`BgBlue`、`BgMagenta`、`BgCyan`、`BgWhite`。
- `Attribute.Sprint(a ...any)` / `Attribute.Sprintf(format, a ...any)` 用于单一属性。
- 用 `NewText(attributes ...Attribute)` 构造的 `Text` 用于组合多个属性。

包裹后的输出形如 `\x1b[<codes>m<text>\x1b[0m`。写入非终端时，需要调用方自行剥除转义序列 —— 本包不做终端检测。

## 关键设计

- **查表，不是终端库。** 所有 ANSI 码直接作为常量写死，整个文件本质上是一次查表 + 一次写入，只覆盖框架需要的小组样式和颜色集。不做光标控制、清屏、256 色 / 真彩色支持；有这类需求请引入 `fatih/color`、`charmbracelet/lipgloss` 等 stdlib 之外的库。
- **两种入口，共用 `wrap`。** `Attribute.Sprint(f)` 用于单属性，`Text.Sprint(f)` 用于组合；两者走同一个 `wrap`，且永远带上 reset。
- **不做 TTY 检测，无全局状态。** 在这个底层工具包里做终端检测会把 `golang.org/x/term` 依赖引进 stdlib；每次调用都无状态，任意 goroutine 都能安全调用。

## License

Apache License 2.0
