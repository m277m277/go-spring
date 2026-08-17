# log benchmarks

Two standalone Go modules that measure the performance of `go-spring.org/log`.
Both are members of the repository's `go.work`, so they build against the
workspace (local `log` and `stdlib`) without extra setup.

## fields/

Module `benchmark-fields` — micro-benchmarks for the `Field` value type that
`log` uses to carry structured context. Four candidate designs are compared
head-to-head on building a field and encoding it to JSON:

| Arm (package)         | Design                                                |
| --------------------- | ----------------------------------------------------- |
| `value_interface`     | field is an interface value                           |
| `value_struct`        | field is a struct with a generic value payload        |
| `field_value`         | flat struct: `Num`/`Str`/`Any` payload per value type |
| `ptr_field_value`     | same flat struct, but constructors return `*Field`    |

Each package additionally verifies its encoding output in a small correctness
test (`TestFieldValueEncode`) so the compared arms are known to agree.

Run from this directory:

```sh
go test -bench . -benchmem ./...
```

## logs/

Module `benchmark-logs` — scenario benchmarks comparing `go-spring.org/log`
against other structured logging libraries in Go (`zap`, `zerolog`, `logrus`,
`apex/log`, `log15`, `go-kit/log`, and the standard library's `log/slog`).
The scenarios mirror zap's upstream benchmark suite: with and without fields,
with accumulated context, with fields added per log site, and level-disabled
variants of each. The go-spring arm refreshes the global logger to a
`DiscardLogger` (via `refreshGSLog`) with level `info` for the enabled
scenarios and `warn` for the disabled ones, so the level gate is measured
without I/O — the same trick as the zap arms' `Discarder` sink.

Run from this directory:

```sh
go test -bench . -benchmem ./...
```

## Further reading

Design notes for the logging library itself live one level up:
[`../README.md`](../README.md) (English) and
[`../README_CN.md`](../README_CN.md) / [`../DESIGN_CN.md`](../DESIGN_CN.md)
(Chinese).
