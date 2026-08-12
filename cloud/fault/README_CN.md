# fault

`fault` 是 [cloud/resilience](../resilience) 的进程内**故障注入**伴生包。它包装一个
`resilience.Executor`,让可配置比例的调用按需失败或变慢——给运行中的客户端"放火"——以验证
重试、熔断、per-attempt 超时、Fallback 真的会触发,并且 observe kit 能记录到相应结果。

## 能力

- 首批一个接缝:`WrapExecutor` —— 包装 executor,让注入的故障落在重试/熔断循环**内部**
  (验证 resilience,而非绕过它)。
- 配置可热刷新(`Injector.SetConfig`),由 starter 的 `gs.Dync[fault.Config]` 绑定驱动——
  运行时切换放火,无需重启。
- 三种注入类型:`generic`(可重试的注入错误)、`timeout`(`context.DeadlineExceeded`)、
  `reset`(`syscall.ECONNRESET`);另有纯延迟模式。
- 注入错误实现 `resilience.Retryable`,无论宿主的谓词如何,都稳定触发重试。
- 仅依赖 stdlib + resilience —— 无第三方依赖,不依赖 gs/spring。

## 安装

```sh
go get go-spring.org/cloud
```

## 用法

包装一个 executor,配置驱动(redigo 在 `setupResilience` 里这么做):

```go
import (
    "go-spring.org/cloud/fault"
    "go-spring.org/cloud/resilience"
)

rawExec, _ := resilience.NewExecutor(driver, policy)
inj := fault.NewInjector(fault.Config{Enabled: true, Rate: 0.5, Error: "generic"})
exec := fault.WrapExecutor(rawExec, inj)        // 最内层
exec = resilobserve.WrapExecutor(exec, ...)     // observe 最外层
```

运行时切换:

```go
inj.SetConfig(fault.Config{Enabled: false})     // 收手
```

配置(各 starter 自带 key 前缀,如 redigo 用 `${fault.*}`):

```properties
fault.enabled=true
fault.rate=0.5
fault.error=generic        # "" | "generic" | "timeout" | "reset"
fault.latency=50ms         # 可选,对每次调用生效
```

注入点的取舍与边界见 [DESIGN_CN.md](DESIGN_CN.md);可运行的负载压测(含放火切换)见
`starter-redigo/example-load`。

## 状态

首批:`WrapExecutor` 接缝 + redigo 试点。Dialer / RoundTripper 接缝与 per-resource 规则
为后续计划(见 [DESIGN_CN.md §4](DESIGN_CN.md))。
