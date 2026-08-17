# md5util
[English](README.md) | [中文](README_CN.md)

`md5util` computes the MD5 checksum of a string and returns it as a lowercase
hex string. Part of Go-Spring's zero-dependency `stdlib` layer.

## Usage

```go
import "go-spring.org/stdlib/md5util"

sum := md5util.MD5("hello") // "5d41402abc4b2a76b9719d911017c592"
```

### API

- `MD5(str string) string` — lowercase hex-encoded MD5 digest.

MD5 is **not** suitable for cryptographic authentication. Use it only for
checksums, cache keys, or fingerprints where collisions are tolerable.

## Design

- One call returns the lowercase-hex form (`encoding/hex.EncodeToString`)
  most consumers need — cache keys, ETags, fingerprints — matching common
  DB / cache conventions. No HMAC, no streaming API: chunked hashing or key
  derivation belongs to `crypto/md5` (or a modern hash) directly.
- One function per package is the point; SHA-1 / SHA-256 / HMAC would get
  their own packages so callers opt in by import — also why this package is
  separate from `hashutil`.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
