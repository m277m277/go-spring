# starter-config-apollo 设计

[English](DESIGN.md) | [中文](DESIGN_CN.md)

一个 config-provider starter（starter/DESIGN.md §2.5）：在
`spring.app.imports` 下注册 `apollo` provider，形态照 starter-config-nacos。

## 1. 职责与边界

- **负责**：provider 注册、source 解析、agollo client 缓存、变更监听→
  刷新桥接、命名空间→properties 解析。
- **不负责**：governance.Source 接入（未来适配器，见 README）、Apollo
  admin/portal 栈。

## 2. 关键抽象与 Seam

- **conf.RegisterProvider("apollo", ctrl.Load)** + **gs.Rooter** — 与 nacos
  相同的双角色 controller：Rooter 获得 autowire 的 `PropertiesRefresher`，
  Load 服务配置拉取。无绑定 Config——连接参数全在 source 串里。
- **clientFor 缓存** — 每个 `(server, appId, cluster, secret, namespace)`
  一个 agollo Client；namespace 进 key 是因为 agollo 在 StartWithConfig
  时就固定 `NamespaceName`。
- **先监听后取** — 与 nacos 共享的承重不变式。

## 3. 约束

- agollo v4 的线路初始同步走 `/configfiles/json/...`（纯 JSON 对象，非
  ApolloConfig envelope）；starter 依赖 agollo 自身的解析，不另写。
- source 里 `appId` 必填（agollo 无它无法拉取）。

## 4. 权衡 / 已否决的方案

- **agollo v4 vs Apollo OpenAPI**：agollo 是官方 Go SDK，自带变更通知长轮
  询；OpenAPI 需手写通知循环。
- **仅冷加载 example vs docker 化 quick-start**：quick-start 要 MySQL +
  configservice/admin/portal 三件套；starter 的契约是 provider seam，mock
  config service 即可端到端覆盖，无需整栈。
