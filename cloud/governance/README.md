# cloud/governance — 服务治理家族

本目录收拢 go-spring 的**运行期服务治理**能力。治理家族的成员各自代表治理的一个维度，彼此是**平级**关系，而非父子从属：

| 子包 | 角色 | 说明 |
|------|------|------|
| `.` (package governance) | **控制面** | 集中式治理中心 `Center`：单一可热更 `Config` → 一次 fan-out。client 注入 `*Center` 调 `PolicyFor`/`Register`。 |
| `resilience/` | **原语** | 防御原语：熔断 / 限流 / 重试 / 退避 / `Executor` 接缝。叶子包，不依赖治理家族其他成员。 |
| `fault/` | **混沌面** | 故障注入（fault injection），借用 `Executor` 接缝注入失败。与 resilience 互为攻防。 |
| `traffic/` | **流量识别面** | 压测/灰度流量标识的识别与传播。 |

## 为什么是平级伞包，而非挂到 resilience 下

`resilience` 是叶子原语，`governance`（控制面）、`fault`（混沌面）、`traffic`（识别面）都**依赖** resilience，是它的消费方。依赖方向为：

```
resilience   ← 叶子原语（不 import 家族其他成员）
governance   ──→ resilience   (PolicyFor 返回 resilience.Policy)
fault        ──→ resilience + traffic
traffic      ← 纯叶子
```

若把 govern/fault 塞进 `resilience/` 当子包，等于宣称"控制面是原语的一种""混沌是原语的一种"——这是假层级，且颠倒了控制方向（govern 驱动 resilience）。中性伞包 `governance` 聚拢它们，既显式化家族关系，又不制造从属。

## 不属于治理家族（仍在 cloud/ 下平级存在）

- `cloud/loadbalance`、`cloud/discovery` — 端点选择与服务注册，与 discovery 成对。
- `cloud/actuator`、`cloud/event`、`cloud/mesh`、`cloud/tlsconf` — 运维/事件/网格/TLS，独立关注点。
- `cloud/loadtest`、`cloud/experimental` — 测试工具与孵化器。
- `cloud/observe` — 可观测性（独立 module，otel 桥，含 `observe/resilience` 等治理桥接）。

## 配置

治理中心绑定在 `${govern}` 配置前缀下（如 `govern.enabled`、`govern.rules`）。该前缀是配置命名空间，与 Go 包名 `governance` 独立。导入本包即生效：

```go
import _ "go-spring.org/cloud/governance" // 注册集中治理中心
```
