# starter-milvus 设计

[English](DESIGN.md) | [中文](DESIGN_CN.md)

Milvus（向量数据库）的 Client 原型 starter，走 gRPC。

## 1. 职责与边界

- **负责**：每实例装配、fail-fast `ListCollections` 探针、健康指示器、连
  接拆除。
- **不负责**：schema 设计（集合/字段/索引是应用的事）、检索/查询语义、
  逐操作韧性（见 §4）。

## 2. 关键抽象与 Seam

- **Client 包装** — 内嵌 SDK 的 `client.Client` 接口；bean 是薄持有者，
  因为 SDK 自己的 client 已是完整面。
- **fail-fast 探针** — 构造期 `ListCollections`；配错地址或凭证在启动期
  就失败，而非首次查询。

## 3. 约束

- 仅 gRPC；无可替换的 HTTP 传输层。

## 4. 权衡 / 已否决的方案

- **无逐操作韧性** — milvus-sdk-go 的 `Config.DialOptions` 仅构建期生效
  （gRPC 拦截器在拨号时挂载，不是 http.RoundTripper 那种逐操作守卫 seam）。
  给每个向量操作包治理执行器，意味着要为整个 `client.Client` 接口手写门面、
  收益却边际——与 cassandra 迭代器路径、MQ 异步路径同立场：只守有自然
  seam 的操作。检索/插入的韧性若需要，归属应用调用点。
