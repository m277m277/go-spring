# fileutil

[English](README.md) | [中文](README_CN.md)

`fileutil` provides two tiny filesystem helpers that plug small gaps in the
standard library's `os` package. Part of Go-Spring's zero-dependency `stdlib`
layer. It is not a filesystem abstraction: no walking, watching, atomic write,
or path manipulation lives here — those are in `os`/`filepath` or in
higher-layer packages.

## Usage

```go
import "go-spring.org/stdlib/fileutil"

ok, err := fileutil.PathExists("/etc/app.conf")
if err != nil {
    return err
}
if !ok {
    // absent
}

names, err := fileutil.ReadDirNames("/var/log/app")
```

### API

- `PathExists(path) (bool, error)` — `(true, nil)` if the path exists,
  `(false, nil)` when it does not, `(false, err)` on any other error (e.g.
  permission denied).
- `ReadDirNames(dirname) ([]string, error)` — names of every entry in
  `dirname`, order defined by the filesystem.

## Design

- `PathExists` collapses the "check for `os.ErrNotExist`" pattern into a
  single call, keeping "does it exist" and "did something go wrong" clearly
  separated: not-exists is expressed as `(false, nil)`, never as an
  `os.ErrNotExist` error; any other stat error is bubbled up untouched.
- `ReadDirNames` reads directory entry names without leaking an `*os.File` —
  it opens, reads, and closes internally. It returns whatever
  `f.Readdirnames(-1)` produces, which may be a partial slice together with a
  non-nil error; callers must check both.

## License

`fileutil` is distributed under the Apache License 2.0. See
[LICENSE](../../LICENSE) for details.
