# funcutil
[English](README.md) | [中文](README_CN.md)

`funcutil` 提供 Go 函数值的运行时元信息（文件、行号、名字），供容器 / 切面
框架拼装人类可读的诊断信息（"bean 注册在 file:line，来自 funcX"）。属于零
依赖的 `stdlib` 层：围绕 `reflect` + `runtime.FuncForPC` 的两函数封装，不是
栈遍历工具——只作用于调用方传入的具体函数值。

## 使用方式

```go
import "go-spring.org/stdlib/funcutil"

func Handle() {}

name := funcutil.FuncName(Handle)
file, line, _ := funcutil.FileLine(Handle)
```

`fn` 必须是函数或方法值，传其它类型会在 `reflect` 内部 panic。

### API

- `FuncName(fn any) string` —— 去掉模块路径前缀后的包限定函数名。
  运行时对方法值打印为 `T.m-fm`，此处会精确去掉尾部 `-fm` 后缀；恰好以
  `-`、`f`、`m` 结尾的函数名不受影响。
- `FileLine(fn any) (file string, line int, fnName string)` —— 源码位置
  加上清理后的函数名。

## 关键设计

- `-fm` 的剥离用 `strings.TrimSuffix`，只切掉完整的方法值后缀，返回的名字
  更接近人写代码的样式。
- 保留最后一段 `/`-分隔的路径（`pkg.Fn`），去掉模块前缀。这是显示层面的
  选择：如果两个不同包里有同名短名字，比较时可能撞上。
- 不做缓存：当前用场（注册期、诊断）频次不高，`*runtime.Func` 反射足够
  便宜，少一层缓存能让这个包保持两个函数一个文件的规模。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
