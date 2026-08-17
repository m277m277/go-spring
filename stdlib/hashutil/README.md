# hashutil
[English](README.md) | [中文](README_CN.md)

`hashutil` is a thin convenience wrapper around `hash/fnv` — a one-function
file in Go-Spring's zero-dependency `stdlib` layer, saving bucketing /
sharding sites the `New64a` + `Write` + `Sum64` triplet.

## Usage

```go
import "go-spring.org/stdlib/hashutil"

h := hashutil.FNV1a64("some/key")
```

### API

- `FNV1a64(s string) uint64` — 64-bit FNV-1a of a string, using the standard
  library `hash/fnv` implementation.

FNV-1a is a fast, non-cryptographic hash. Suitable for map sharding, cache
bucketing, and similar tasks. Do not use it where an adversary can choose
inputs.

## Design

- Delegates to `hash/fnv` rather than inlining the FNV-1a loop — readability
  and consistency with other `hash.Hash` users beat a few nanoseconds; no
  streaming API, feed chunks via `hash/fnv` directly.
- Not a cryptographic hash package: MD5 lives in `md5util`; SHA family and
  HMAC belong outside stdlib if they ever land.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
