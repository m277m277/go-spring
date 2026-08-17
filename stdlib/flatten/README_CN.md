# flatten
[English](README.md) | [中文](README_CN.md)

`flatten` 把 JSON 结构的嵌套数据打平成 `key -> string`，并提供 Go-Spring 配置
绑定器所依赖的 `Storage` 抽象。属于零依赖的 `stdlib` 层。它不是配置加载器：
不会读文件、环境变量或命令行，来源由调用方自己拼装 `Properties` 并放入
某一层。

## 使用方式

```go
import "go-spring.org/stdlib/flatten"

flat := flatten.Flatten(map[string]any{
    "server": map[string]any{"port": 8080, "host": "localhost"},
    "users":  []any{map[string]any{"name": "tom"}},
})
// flat == {"server.port":"8080","server.host":"localhost","users[0].name":"tom"}

path, err := flatten.SplitPath("server.port")
_ = path // [{key server} {key port}]
_ = flatten.JoinPath(path)

s := flatten.NewPropertiesStorage(flatten.NewProperties(flat))
v, _ := s.Value("server.port")
```

### API

- `Flatten(map[string]any) map[string]string` —— 用点号 / 方括号
  （`a.b`、`a[0]`）打平嵌套 map 和切片。
- `Path`、`JoinPath`、`SplitPath` —— 层级 key 的解析与拼接。
- `Properties` / `PropertiesStorage` —— 扁平化 `key -> string` 存储，实现
  `Storage` 接口，供绑定器使用。
- `PrefixedStorage` —— 透明地为所有 key 增加前缀。
- `LayeredStorage` —— 按固定优先级组合多个配置源
  （`StorageCommandLine`、`StorageEnvironment`、`StorageProfileFile`、
  `StorageAppFile`、`StorageDefault`）。

### 扁平化规则

- 嵌套 map 用 `.` 展开：`{"a":{"b":1}}` -> `"a.b"="1"`。
- 切片用 `[i]` 展开：`{"a":[1,2]}` -> `"a[0]"="1"`、`"a[1]"="2"`。
- 无类型或有类型的 `nil` 都表示为 `"<nil>"`。
- 非 nil 但为空的 map 表示为 `"{}"`，空切片表示为 `"[]"`。
- 基本类型走 `strconv` 格式化。

## 关键设计

- `Flatten` 是面向展示的单向转换，**不可逆**：供日志、对比、诊断以及
  `Storage` 输入使用；只支持 JSON 原生类型（map/slice/基本类型/nil），
  结构体、非字符串 map key、自定义类型都被显式排除。
- `Path` + `Split/JoinPath` 提供 key 路径的可往返表示；`Storage` 接口保持
  最小——`Value` / `MapKeys` / `SliceEntries` 三种绑定能力加上属性条件判断
  用的 `Exists`——方便接入远程配置等替代实现。
- `LayeredStorage` 有意混用两种覆盖规则：叶子值与切片高优先级层胜出，上层
  定义同名切片后下层的部分切片被整体遮蔽（`my.list[0]=c` 压过 `[a,b]` 得到
  `[c]` 而非 `[c,b]`）；map 则跨层合并 key、叶子值仍按覆盖解析。非对称是有意
  的——合并数组语义不清，合并 map key 才是调用方期望的形态。
- `PrefixedStorage.SliceEntries` 会把自己加的前缀从返回 key 上剥掉，保证调用
  方看到的是自己的命名空间；`LayeredStorage.Data()` 是自省快照（例如
  actuator 的 env 端点），不是绑定路径。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
