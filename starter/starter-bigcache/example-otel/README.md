# starter-bigcache OTel Example

Surfaces BigCache statistics as OpenTelemetry gauges and scrapes them via the
Prometheus pull exporter.

## What it shows

- `starter-bigcache` registers OTel observable gauges for each BigCache instance
  (`bigcache.hits`, `bigcache.misses`, `bigcache.delete_hits`,
  `bigcache.delete_misses`, `bigcache.collisions`, `bigcache.entries`,
  `bigcache.capacity`), labeled by the instance name (`cache.name`).
- `starter-otel` serves the gauges at the in-process Prometheus endpoint
  (`:9090/metrics`). No external collector is required.

The example generates 20 SET/GET (hits) + 5 GET on absent keys (misses), then
scrapes `/metrics` and asserts the `bigcache_hits` / `bigcache_misses` gauges
appear with `cache_name="hot"` and that the hit gauge is non-zero.

## Run

```bash
cd starter-bigcache/example-otel
go run .
```

## Manual mode

```bash
go run . -manual
# then, in another terminal:
curl http://localhost:9090/metrics | grep bigcache
```

## Smoke test

```bash
./check.sh
```
