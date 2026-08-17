# spring 设计说明
[English](DESIGN.md) | [中文](DESIGN_CN.md)

`go-spring.org/spring` 是 Go-Spring 分层栈（stdlib → log → spring →
cloud → starter）中的**核心层**，提供 IoC 容器、依赖注入接线、分层配置
引擎与应用生命周期模型——仅此而已。它只依赖 `stdlib` 与 `log`，绝不
import `cloud` 生态库、任何三方业务 SDK 或任何 starter。

## 1. 职责与边界

- 容器职责：在 `init()` 阶段收集 bean 定义，解析条件，接线依赖，跑
  `Init` / `Destroy` 生命周期，由 `gs.Run()` 驱动 `Runner` / `Server` 两
  个角色。容器不承担协议逻辑，也不持有三方客户端——它把接缝
  （`Provide`、`Module`、`Group`、`Condition`）暴露给 starter 去贡献。
- `spring/conf` 是配置引擎：分层来源（命令行、环境变量、
  `app-<profile>.<ext>`、`app.<ext>`、内存、tag 默认值）按优先级合并，
  格式读取器在 `spring/conf/reader/{yaml,toml,prop,json}`，可插拔解密在
  `spring/conf/decrypt`。引擎独立于容器，容器在启动过程中驱动它。它也
  是外部生态唯一直接复用的 spring 包——如 starter-governance 的规则解
  析胶水就构建在它之上。
- `spring/gs` 是对外表面：`Provide`、`Configure`、`Module`、`Group`、
  `OnProperty`、`OnBean`、`Dync[T]`、`Runner`、`Server`、`ReadySignal`、
  `PropertiesRefresher`。`spring/gs/internal/...` 全是实现细节，不对
  外承诺。

### 能力家族去了哪里

`spring/` 本体只含 `gs/` 与 `conf/`——纯核心。所有能力抽象都按依赖方向
迁到了该在的层：

| 家族 | 归属 | 理由 |
|---|---|---|
| `httpsvr`、`httpclt` | `stdlib/` | 纯 HTTP 语义，无容器依赖 |
| 治理（`resilience`/`fault`/`traffic`）、`discovery`、`loadbalance`、`event`、`scheduling`、`batch`、`lock`、`messaging`、`transaction`、`tlsconf`、`cache`、`repository`、`migration`、`i18n`、`validation`、`httpx`、`security`、`session` | `cloud/`（go-spring.org/cloud） | 生态抽象，容器无关——cloud 模块不 import 任何 spring 包 |
| 具体后端（Redis、GORM、Kafka、dubbo-go…） | `starter/` | 三方 SDK + gs 接线 |

全仓遵循的口径：**抽象进 cloud，gs 接线与三方 SDK 进 starter，无生态
依赖的纯语义进 stdlib**——starter 负责接线，cloud 负责抽象，spring 负责
运行。

## 2. 关键抽象与接缝

- **Bean 注册**。`gs.Provide(objOrCtor, args...)` 在 `init()` 期记下
  bean 定义。构造函数的参数按类型索引进行匹配。链式 builder 配置
  `Name`、`Init` / `Destroy`、`Condition`、`DependsOn`、`Export`、
  `Configuration`。
- **按导出接口建索引**。容器为每个 bean 建两份索引：bean 自身的具体类
  型，以及 `.Export(gs.As[Iface]())` 声明的每个接口。**未在 Export 里
  列出的接口不会被索引**——就算 bean 结构上实现了该接口，
  `[]Iface autowire:""` 和 `gs.OnBean[Iface]()` 也找不到它。参见
  `spring/gs/internal/gs_core/injecting/injecting.go`（`beansByType`、
  `GetExports`）。bean 必须是引用类型（指针/接口），值结构体不能做
  bean；同类型多 bean 必须 `.Name()`，否则默认名冲突报重复。
- **依赖注入**。字段上的 `autowire:""` / `autowire:"name?"` /
  `autowire:"a,*?,b"` 与 `value:"${key:=default}"` 在一次反射遍历里全部
  填好。启动后不再反射——匹配函数与字段偏移都被缓存。
- **条件化自动装配**。`gs.Module(cond, fn)` 把 starter 的一组 bean 收在
  `PropertyCondition` 后面；`gs.OnProperty` / `gs.OnBean` /
  `gs.OnMissingBean` / `gs.OnSingleBean` 通过 `And` / `Or` / `Not` /
  `None` 组合。这是每个 starter 从配置键选择性启用自己的统一接缝。
- **动态配置**。`gs.Dync[T]` 包住字段，让
  `PropertiesRefresher.RefreshProperties()` 就地重绑而无需重启容器。这
  是配置中心类 starter（`starter-config-{nacos,etcd,consul,vault,file}`）
  共用的接缝。
- **运行时模型**。只有两个角色：一次性的 `Runner` 与长时运行的
  `Server`（带 `ReadySignal`）。所有 server 先完成监听绑定，再阻塞在
  `sig.TriggerAndWait()`，保证任何 server 都不会在其它 server 尚未绑
  定端口时就接流量。框架负责并发启动、信号处理与优雅 `Stop()`。

## 3. 约束

- `spring/` 内禁止出现三方业务依赖。Go 标准库、`stdlib/` 与 `log/` 之外
  的一切归 `cloud/`（生态抽象）或 `starter/`（后端 + gs 接线）。
- 所有注册都在 `init()` 期完成。`Configure(func(app gs.App))` 是这个阶
  段的延伸；`Run` 开始后不允许再注册 bean。
- `internal/` 子树不属于对外 API，即便通过再导出可达；下游必须走 `gs.`
  包。
- bean 暴露的接口就等于 `.Export(gs.As[Iface]())` 显式声明的那些——不
  做自动接口发现。漏 `Export` 是最常见的接线 bug，且在收集阶段静默失
  败。

## 4. 权衡与被否决的方案

下面的否决不是各自独立的取舍，而是同一条立场：**容器只做装配，不做
裁决。** Java Spring 扩展点的膨胀源于本项目并不承受的三种力——服务于
寄宿在 Spring *之上*的数千框架的钩子（BeanPostProcessor 存在的理由是
让代理能替换用户的 bean）、为偿还语言限制而搬进容器的复杂度（无一等
装饰能力、类型擦除）、以及二十年不允许做减法的向后兼容。Go 的一等函
数让装饰成为显式嵌套；配置键激活让"什么时候装配"不再是问题。某个家
族需要接入时，得到的是类型化的 seam（`.Export(gs.As[...])` 收集、
Driver 注册表）——面向该家族的契约，而不是能改写任意 bean 的钩子。

- **拒绝运行时扫描（Spring Boot 那样的 classpath 扫描）**。所有 bean 元
  数据都在 `init()` 里注册，没有 classpath 遍历。代价是每个 bean 提供方
  包必须被 `main` 直接或间接 import，否则其 `init()` 不会执行、bean 会
  静默缺失；收益是可预测的启动、无顺序玄学、接线后零反射。
- **拒绝 `autoconfig.exclude`**。Spring Boot 之所以需要排除清单，是因为
  自动配置按 classpath 存在性激活——急切且隐式，只能另找一个地方放关掉
  它的开关。这里所有 starter 经配置键条件注册
  （`gs.Module(gs.OnProperty("spring.X"), ...)`）：import 一个 starter 在
  配置出现之前不装配任何东西，所以"禁用"就是"不配置"；少数开关型便利
  （如 `spring.pprof.enabled`）自带显式开关。代价：starter 作者必须把注
  册挂在配置键条件上——不允许 `init()` 里无条件注册；收益：开关只有一
  个，就在运维本来就会看的配置里，不存在需要同步维护的第二份漂移开关。
- **拒绝 `@Primary`**。注入以名字为先（`autowire:"name"`），Spring 里
  `@Primary` 解决的"按类型多候选"歧义在这里基本不会出现；当默认 bean 需
  要让位于用户提供的 bean 时，用条件作用在默认 bean 的**注册**上表达——
  `gs.OnMissingBean[T]()`、`gs.OnSingleBean[T]()`、`gs.OnProperty(...)`，
  决定的是"这个 bean 存不存在"，而不是"已有的几个里谁赢"。代价：带默认
  实现的 starter 必须显式挂条件，不能靠优先级标记；收益：类型索引里没有
  隐藏的优先级层——依赖图保持按名寻址，整个决策在注册期可见。
- **拒绝编译期 DI（Wire 那种代码生成）**。容器保留运行时依赖图，让条件
  模块、从配置 map 里批量建 bean 的 `Group`、`Dync[T]` 热刷新可以在启
  动/运行期决定实例化什么。反射被约束在启动那一遍。
- **拒绝隐式接口索引**。若把每个"结构上实现"的接口都索引进来，
  `OnBean[Iface]` 与 `[]Iface autowire:""` 就变得非局部、难以推理。强
  制 `Export` 让类型索引成为维护者掌控下的闭集合。
