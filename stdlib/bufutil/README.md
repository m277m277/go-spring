# bufutil

[English](README.md) | [中文](README_CN.md)

`bufutil` provides bounded buffers for side-channel copying, where the copy must
never backpressure or error the primary flow. A `LimitedBuffer` wraps a
`bytes.Buffer` with a byte cap: writes past the cap are silently discarded, but
`Write` always reports the full size and a nil error. Use it whenever you need a
best-effort copy of a stream alongside its primary consumer — losing the tail is
acceptable, slowing down or failing the primary flow is not: access-log body
capture (mirroring a request/response body via `io.TeeReader`, as `starter-gin`
does), streaming/SSE chunk capture (one buffer reused via `Reset()` per event),
or any debug/audit echo where truncation is fine and the copy must never become
the failure point. Do **not** use it when you need all the bytes, exact byte
accounting, flow control, or concurrent writes.

## Usage

Capture a body via `io.TeeReader` without risking memory exhaustion:

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

The bound sits on the write side of the side copy, which is what keeps the
primary flow untouched:

| Type | Bound side | Overflow behavior | Primary flow affected? |
|---|---|---|---|
| `bytes.Buffer` | none | grows unboundedly | no (but memory risk) |
| `io.LimitReader` | read side | primary reader gets a truncated stream | **yes** |
| `LimitedBuffer` | write side | side copy truncated, silently | no |

- **Silent overflow + full-write lie**: the defining property. `Write` drops
  bytes past the cap yet returns `len(p), nil`. This is what keeps an
  `io.TeeReader` unblocked when the capture is full — the alternative (erroring
  or short-writing) would corrupt the primary read.
- **`NewLimitedBuffer(max)` constructor, zero-value = cap 0**: a zero-value
  `LimitedBuffer` discards everything, which is a safe default; callers must opt
  in to a non-zero cap. Negative caps panic (programmer error, not a runtime
  condition).
- **`Bytes()` aliases internal storage**: inherited from `bytes.Buffer`; the
  slice is invalidated by the next `Write`/`Reset`. Callers that need a stable
  copy take it before the next write.
- **Lossy by design**: data past the cap is gone with no record of how much was
  dropped. Callers that need exact-byte accounting must track it themselves.
- **Truncation is byte-boundary, not rune-boundary**: a multi-byte UTF-8 rune
  (Chinese, emoji) can be split at the tail, leaving invalid UTF-8 in
  `Bytes()`/`String()`. The damage is strictly local — UTF-8 is
  self-synchronizing, everything before the cut stays correct — so sanitizing is
  the consumer's opt-in (`strings.ToValidUTF8`), not a scan built into every
  capture.
- **Not safe for concurrent use**, mirroring `bytes.Buffer`. The cap is set once
  at construction and never grows; `Reset` clears contents but keeps the cap.

## License

`bufutil` is distributed under the Apache License 2.0. See
[LICENSE](../../LICENSE) for details.
