# starter-milvus Design

[English](DESIGN.md) | [中文](DESIGN_CN.md)

A Client-archetype starter for Milvus (vector database) over gRPC.

## 1. Responsibilities & Boundaries

- **Owns**: per-instance wiring, the fail-fast `ListCollections` probe, the
  health indicator, connection teardown.
- **Does not own**: schema design (collection/field/index are the app's),
  search/query semantics, and per-operation resilience (see §4).

## 2. Key Abstractions & Seams

- **Client wrapper** — embeds the SDK's `client.Client` interface; the bean is
  a thin holder because the SDK's own client is already the full surface.
- **Fail-fast probe** — `ListCollections` at construction; a wrong address or
  bad credential fails at boot, not on first query.

## 3. Constraints

- gRPC-only; there is no HTTP transport to swap.

## 4. Trade-offs / Alternatives Rejected

- **No per-operation resilience** — milvus-sdk-go's `Config.DialOptions` is
  build-time only (gRPC interceptors are attached at dial, not a per-op guard
  seam like an http.RoundTripper). Wrapping every vector op in the governance
  executor would force a hand-written facade over the whole `client.Client`
  interface for marginal benefit — the same stance as the cassandra
  iterator-path and the MQ async paths: guard only the operations with a
  natural seam. Search/insert resilience, if needed, belongs to the
  application's call sites.
