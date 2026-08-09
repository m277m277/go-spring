# starter-bigcache OTel 示例

把 BigCache 统计作为 OpenTelemetry gauge 暴露，通过 Prometheus 拉取式 exporter 采集。

## 展示什么

- `starter-bigcache` 为每个 BigCache 实例注册 OTel observable gauge
  （`bigcache.hits`、`bigcache.misses`、`bigcache.delete_hits`、
  `bigcache.delete_misses`、`bigcache.collisions`、`bigcache.entries`、
  `bigcache.capacity`），按实例名打 label（`cache.name`）。
- `starter-otel` 在进程内 Prometheus 端点（`:9090/metrics`）暴露这些
  gauge，无需外部 collector。

示例生成 20 次 SET/GET（命中）+ 5 次读不存在的 key（未命中），然后抓取
`/metrics`，断言 `bigcache_hits` / `bigcache_misses` gauge 以 `cache_name="hot"`
出现，且命中 gauge 非零。

## 运行

```bash
cd starter-bigcache/example-otel
go run .
```

## 手动模式

```bash
go run . -manual
# 另开一个终端：
curl http://localhost:9090/metrics | grep bigcache
```

## 冒烟测试

```bash
./check.sh
```
