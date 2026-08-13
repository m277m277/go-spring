# cloud/traffic — starter rollout status

> Load-test traffic identification propagation across the go-spring starter
> ecosystem. Status as of 2026-08-13.

## What "propagation" means here

`cloud/traffic` tags a request/context as load-test traffic and ferries that
marker across process boundaries so every hop can tell synthetic load from real
traffic (`traffic.IsLoadTest(ctx)`). Each transport needs:

- **outbound inject** — when the caller's ctx is tagged, write the marker into
  the outgoing carrier (HTTP header / gRPC metadata / message headers).
- **inbound extract** — read the marker off the incoming carrier and tag the
  handler ctx.

## Covered (12 transports)

| starter | outbound | inbound | carrier |
|---|---|---|---|
| starter-http-client (via cloud/httpx) | ✅ | — | HTTP header `X-LoadTest` |
| starter-gin | — | ✅ | HTTP header (middleware, outermost) |
| starter-echo | — | ✅ | HTTP header (middleware, outermost) |
| starter-hertz | — | ✅ | HTTP header (middleware, outermost) |
| starter-grpc | — | ✅ | gRPC metadata `x-loadtest` (interceptor, chain head) |
| starter-kafka | ✅ | ✅ | record header (recordCarrier) |
| starter-kafka-sarama | ✅ (call-site) | ✅ (call-site) | record header (producer/consumerCarrier) |
| starter-rabbitmq | ✅ | ✅ | `amqp.Publishing.Headers` |
| starter-nats | ✅ | ✅ | `nats.Header` (= http.Header) |
| starter-pulsar | ✅ | ✅ | `ProducerMessage.Properties` (map[string]string) |
| starter-trpc | — | ✅ | server metadata `map[string][]byte` (registered filter "loadtest") |
| starter-dubbo | — | ✅ | invocation attachment (registered filter "loadtest") |

### Activation notes
- HTTP servers (gin/echo/hertz) + grpc: on by default, toggle via
  `<server>.loadtest.enabled` (and `.header` for the HTTP trio).
- MQ binders (kafka/kafka-sarama/rabbitmq/nats/pulsar): always on, inert unless
  the producer's ctx is tagged.
- trpc/dubbo: register-on-by-default, but the filter must be added to the
  service's filter chain to run (config-driven named-filter model, same as the
  built-in tracing/metrics filters).

## Not covered — carrier-limited (same boundary as trace)

These starters do NOT propagate the load-test marker, for the same reason their
**trace** propagation is limited: the underlying library/protocol exposes no
per-call metadata carrier at the starter seam. They are not regressions — they
match the existing observability boundary.

- **starter-kitex** — kitex v0.16.3 exposes no public server-side metainfo read
  API (the `pkg/metainfo` package is absent; `pkg/transmeta` is a wire-level
  internal handler with no stable accessor). Revisit when a public
  `metainfo.GetMetaValue` lands or via a custom `remote.MetaReadHandler`.
- **starter-thrift** — apache/thrift Go has no native cross-service header
  propagation; the transport header is protocol-specific and out of scope (the
  starter's trace filter starts a root span for the same reason). `IsLoadTest`
  still works in-process.
- **starter-mqtt** — MQTT 3.1.1 (paho v3 client) packets carry no per-message
  metadata; the binder's own docs note trace cannot propagate either. Needs
  MQTT v5 user properties to support it.

## Consumer side (shadow routing) — intentionally not built

Identifying load-test traffic is the framework's job; **acting** on it (shadow
table / shadow key / isolated breaker pool) is intentionally left to a
user-added layer, because it is business-specific. The marker is readable at
every layer via `traffic.IsLoadTest(ctx)` / `traffic.Source(ctx)`; a consumer
checks it and rewrites its target. See the design doc for the onion-model
rationale.

## Server-side fault injection (direction symmetry)

`cloud/fault` originally only gated outbound/client calls via `WrapExecutor`
(inside a resilience Executor). A second seam, `fault.Apply`, now lets a server
inject faults into INBOUND traffic — the same `Injector` config + Scope rule +
MaxDuration/MaxAffected guardrails, applied around the handler. Coverage:

| starter | seam | activation |
|---|---|---|
| starter-gin | `buildFault` middleware | `${spring.gin.server.fault.enabled}` |
| starter-echo | `buildFault` middleware | `${spring.echo.server.fault.enabled}` |
| starter-hertz | `buildFault` middleware | `${spring.hertz.server.fault.enabled}` |
| starter-grpc | `FaultUnary/StreamInterceptor` | `${spring.grpc.server.fault.enabled}` |
| starter-trpc | registered filter "fault" | add "fault" to service filter chain |
| starter-dubbo | registered filter "fault" | `SetFaultInjector` + add "fault" to provider filter chain |

Injected faults surface as 503 (HTTP) / injected error (RPC), and (for grpc/trpc)
flow back through tracing/metrics so the fault is observed. gin/echo/hertz/grpc
build the injector once at startup from static server config; trpc/dubbo are
config-driven named-filter models (same as their tracing/metrics filters).

