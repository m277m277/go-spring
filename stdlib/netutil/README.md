# netutil
[English](README.md) | [中文](README_CN.md)

`netutil` provides tiny network helpers for use inside the Go-Spring
framework — currently just the local-IPv4 lookup used by registration and
startup logs. Part of the zero-dependency `stdlib` layer, not a networking
framework — no interface enumeration, CIDR matching, or address parsing
beyond `net` itself.

## Usage

```go
import "go-spring.org/stdlib/netutil"

ip := netutil.LocalIPv4()
```

### API

- `LocalIPv4() string` — the first non-loopback IPv4 address of the local
  machine, or `"0.0.0.0"` when none is available. Cached after the first
  call.

## Design

- One call answers "how do I appear on the network?" for service
  registration, log tagging, and actuator info.
- The `sync.Once` cache keeps the answer stable for the process lifetime,
  matching the framework's assumption that host addressing is fixed after
  boot; later interface changes are not observed.
- Errors from `net.InterfaceAddrs()` are swallowed behind the `"0.0.0.0"`
  sentinel to keep the API string-typed — the trade-off is silent
  misconfiguration. Callers needing hard failures should probe
  `net.InterfaceAddrs` directly.
- IPv4-only: most Go-Spring consumers (Nacos / etcd / Consul style discovery,
  log lines) still key on IPv4.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
