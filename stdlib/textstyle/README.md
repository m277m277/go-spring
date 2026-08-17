# textstyle
[English](README.md) | [中文](README_CN.md)

`textstyle` wraps strings with ANSI escape codes for colored / styled terminal output. It always emits the codes and never detects whether the destination is a terminal, which suits CLI tooling and log helpers that already know whether styling is appropriate. Part of Go-Spring's zero-dependency `stdlib` layer.

## Usage

```go
import "go-spring.org/stdlib/textstyle"

fmt.Println(textstyle.Red.Sprint("error: connection refused"))
fmt.Println(textstyle.NewText(textstyle.Bold, textstyle.Green).
    Sprintf("ok %d/%d", n, total))
```

### Features

- Style attributes: `Bold`, `Italic`, `Underline`, `ReverseVideo`, `CrossedOut`.
- Foreground colors: `Black`, `Red`, `Green`, `Yellow`, `Blue`, `Magenta`, `Cyan`, `White`.
- Background colors: `BgBlack`, `BgRed`, `BgGreen`, `BgYellow`, `BgBlue`, `BgMagenta`, `BgCyan`, `BgWhite`.
- `Attribute.Sprint(a ...any)` / `Attribute.Sprintf(format, a ...any)` for single attributes.
- `Text` type built from `NewText(attributes ...Attribute)` for combined attributes.

Wrapped output is `\x1b[<codes>m<text>\x1b[0m`. When targeting a non-terminal writer, callers should strip the sequences themselves — this package does not detect terminals.

## Design

- **A lookup table, not a terminal library.** Every ANSI code is a hard-coded constant — the whole file is one lookup plus one write, covering only the small, common set of styles and colors the framework needs. No cursor movement, clearing, or 256-color / true-color support; for those reach for `fatih/color` or `charmbracelet/lipgloss` outside stdlib.
- **Two entry shapes, one `wrap`.** `Attribute.Sprint(f)` serves a single attribute, `Text.Sprint(f)` serves combinations; both funnel through the same `wrap` helper, which never omits the reset.
- **No TTY detection, no global state.** Detecting terminals in this leaf utility would push a dependency (`golang.org/x/term`) into stdlib; every call is stateless and safe from any goroutine.

## License

Apache License 2.0
