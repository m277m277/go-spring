# fileutil

[English](README.md) | [中文](README_CN.md)

`fileutil` 提供两个针对 `os` 包空白点的文件系统小工具，属于 Go-Spring 零依赖的 `stdlib` 层。它不是文件系统抽象层：遍历、监听、原子写、路径拼接都不在这里——它们要么在 `os` / `filepath` 中，要么属于更上层的包。

## 使用方式

```go
import "go-spring.org/stdlib/fileutil"

ok, err := fileutil.PathExists("/etc/app.conf")
if err != nil {
    return err
}
if !ok {
    // 不存在
}

names, err := fileutil.ReadDirNames("/var/log/app")
```

### API

- `PathExists(path) (bool, error)` —— 路径存在返回 `(true, nil)`，不存在
  返回 `(false, nil)`，出现其它错误（如权限不足）返回 `(false, err)`。
- `ReadDirNames(dirname) ([]string, error)` —— 返回目录下所有条目名，
  顺序由文件系统决定。

## 关键设计

- `PathExists` 把"判断 `os.ErrNotExist`"的常见模式收敛为单次调用，让
  "是否存在"与"是否发生错误"两个语义清晰分开："不存在"用 `(false, nil)`
  表达，永远不会作为 `os.ErrNotExist` 错误抛出；其它 stat 错误原样返回。
- `ReadDirNames` 读取目录条目名而不泄漏 `*os.File`——内部开、读、关一气
  呵成。它直接返回 `f.Readdirnames(-1)` 的结果，可能返回部分切片加上
  非空 err，调用方需要同时检查两者。

## License

`fileutil` 基于 Apache License 2.0 发布，详见 [LICENSE](../../LICENSE)。
