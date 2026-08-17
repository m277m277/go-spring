# bufutil

[English](README.md) | [中文](README_CN.md)

`bufutil` provides a bounded buffer for copying a data stream on the side of its
primary flow. Dropping the tail of the copy is fine; slowing down or erroring the
primary flow is not.

`LimitedBuffer` is a `bytes.Buffer` with a byte cap: writes past the cap are
silently dropped, but `Write` still reports the full size and a nil error.
Typical uses are access-log capture of a request/response body (mirrored via
`io.TeeReader`), streaming/SSE chunk capture (one buffer reused with `Reset()`
between chunks), and other debug/audit echoes where truncation is harmless.

If you need every byte, exact byte accounting, flow control, or concurrent
writes, it's the wrong tool.

## Usage

Capture a body via `io.TeeReader`:

```go
import (
    "io"
    "go-spring.org/stdlib/bufutil"
)

// Mirror the request body into a bounded capture for the access log. The handler
// still reads every byte; the capture keeps at most 512 KiB.
capture := bufutil.NewLimitedBuffer(512 * 1024)
body = io.TeeReader(r.Body, capture)
// ... handler reads `body` ...
log.Printf("req.body=%s", capture.String())
```

For a streaming response, reuse one buffer across chunks:

```go
buf := bufutil.NewLimitedBuffer(l.limit)
for each event {
    buf.Reset() // keep the cap, drop the previous chunk
    io.Copy(buf, eventReader)
    log.Printf("event=%s", buf.String())
}
```

### API

| Method | Description |
|---|---|
| `NewLimitedBuffer(max int) *LimitedBuffer` | Create a buffer that keeps at most `max` bytes (panics if `max < 0`). |
| `Write(p []byte) (int, error)` | Append up to the cap, drop overflow; always returns `len(p), nil`. |
| `WriteString(s string) (int, error)` | String convenience over `Write`. |
| `Bytes() []byte` | Buffered bytes (aliases internal storage). |
| `String() string` | Buffered bytes as a string. |
| `Len() int` / `Cap() int` | Current / max size. |
| `Reset()` | Discard contents (keeps the cap) for reuse. |

## Design

- **Silent overflow, full-write report**: `Write` drops bytes past the cap but
  still returns `len(p), nil`. That keeps an `io.TeeReader` unblocked when the
  capture fills — erroring or short-writing would corrupt the primary read
  instead.
- **Zero-value = cap 0**: a zero-value `LimitedBuffer` discards everything, a
  safe default; use `NewLimitedBuffer(max)` to opt into a non-zero cap. A
  negative cap panics.
- **`Bytes()` returns an alias to internal storage**: inherited from
  `bytes.Buffer`, the slice goes stale on the next `Write`/`Reset`. Copy it
  before the next write if you need a stable snapshot.
- **Lossy**: data past the cap is dropped with no record of how much. Track the
  byte count yourself if you need it exact.
- **Truncates on byte, not rune**: a multi-byte UTF-8 rune (Chinese, emoji) can
  be split at the tail, leaving invalid UTF-8 in `Bytes()`/`String()`. Everything
  before the cut stays intact (UTF-8 is self-synchronizing); if you need a clean
  string, run `strings.ToValidUTF8` once yourself.
- **Not safe for concurrent use**, same as `bytes.Buffer`. The cap is fixed at
  construction and never grows; `Reset` clears contents but keeps the cap.

## License

`bufutil` is distributed under the Apache License 2.0. See
[LICENSE](../../LICENSE) for details.
