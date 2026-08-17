# formutil
[English](README.md) | [中文](README_CN.md)

`formutil` provides generic encode / decode helpers between Go values and
form-style key-value maps (`url.Values`, `[]string`), used by the Go-Spring
HTTP client / server binding code. Part of the zero-dependency `stdlib`
layer; not a validator — range checking is the only cross-cutting rule here,
everything else is delegated.

## Usage

```go
import (
    "net/url"
    "go-spring.org/stdlib/formutil"
)

// Decode a single value
n, err := formutil.DecodeInt[int]("page", []string{"3"})

// Decode repeated values
ids, err := formutil.DecodeList("ids",
    []string{"1", "2", "3"}, formutil.DecodeInt[int64])

// Encode into url.Values
v := url.Values{}
_ = formutil.EncodeString(v, "name", "alice")
_ = formutil.EncodeIntPtr[int64](v, "opt", nil) // omitted
```

### API

- Symmetric `Decode<Type>` / `Encode<Type>` pairs for `bool`, signed and
  unsigned integers, floats, `string`, byte slices, and arbitrary JSON.
- `<Type>Ptr` variants (except `Bytes` / `JSON` / `List`) that treat `nil` as
  "absent" on encode and return `*T` on decode.
- Generic `DecodeList` / `EncodeList` for repeated form fields.
- Overflow-safe integer / float decoding via `stdlib/mathutil`.
- JSON encoding delegates to `stdlib/jsonflow`.

### Rules

- Every non-list decoder rejects more than one raw value ("too many values
  for form field ...") and rejects an empty value list ("missing value for
  form field ...").
- Integer / unsigned / float decoders return a range error when the parsed
  value would not fit `T`.
- `DecodeBytes` / `EncodeBytes` use standard base64.
- `EncodeXxxPtr` omits the field entirely when the pointer is `nil`.

## Design

Single-field, primitive helpers only — struct-level binding stays in the
caller; encode/decode pairs are symmetric so the code that emits a field can
parse it back.

- Generic functions (`Decode/EncodeInt[T]`) instead of type-per-file: flat
  surface, one generator-targetable function per field type.
- Decode input is `[]string` to match `url.Values[key]`; a `nil` pointer
  means absent on encode, so a binder can tell "unset" from "zero" without a
  bitmap.
- Base64 for bytes and JSON via `stdlib/jsonflow` lock in canonical wire
  formats, so a binder pair agrees by construction.
- Constraints: zero non-`stdlib` dependencies; floats always format as
  `strconv.FormatFloat(..., 'f', -1, 64)`, losing precision info for
  `T = float32`; overflow errors are plain `errutil.Explain` strings, not
  typed sentinels.

## License

Apache License 2.0. See [LICENSE](../../LICENSE).
