# starter-gorm-sqlite

[English](README.md) | [中文](README_CN.md)

`starter-gorm-sqlite` is the SQLite dialect of the gorm starter family: one
gorm client per entry under `spring.gorm.sqlite.<name>`, with the shared
pool/observe/resilience/health scaffolding from `go-spring.org/starter-gorm`.
It uses [glebarez/sqlite](https://github.com/glebarez/sqlite) (pure Go, no
CGO) over [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite).

## Installation

```bash
go get go-spring.org/starter-gorm-sqlite
```

## Quick Start

### 1. Import

```go
import _ "go-spring.org/starter-gorm-sqlite"
```

### 2. Configure

```properties
spring.gorm.sqlite.primary.file=:memory:
spring.gorm.sqlite.primary.journal-mode=wal
spring.gorm.sqlite.primary.busy-timeout=5000
spring.gorm.sqlite.primary.max-open-conns=1
```

### 3. Inject

```go
type Service struct {
    DB *StarterGormSqlite.DB `autowire:"primary"`
}
```

### 4. Use

```go
err := s.DB.AutoMigrate(&Model{})
err = s.DB.Create(&Model{Name: "x"}).Error
```

`DB` aliases the shared `gormcore.DB`, so the full gorm API promotes
unchanged, with the per-instance health indicator (`gorm:sqlite:<name>`)
folded into actuator readiness.

## Configuration

| Key | Default | Notes |
| --- | --- | --- |
| `file` | — (required) | database path, or `:memory:` |
| `journal-mode` | `wal` | wal / delete / truncate / persist / memory / off |
| `busy-timeout` | `5000` | milliseconds (`_busy_timeout` pragma) |
| `foreign-keys` | `true` | enable the `foreign_keys` pragma |

Plus the shared gormcore pool/observe keys (`max-open-conns`, `slow-threshold`,
`observe.enabled`, ...) — see the starter-gorm README. `:memory:` gives each
connection its own store, so pin `max-open-conns=1` for a stable in-memory
round trip.

## Compatibility

SQLite is an in-process database — there is no TLS or service discovery to
configure, and no docker image to run for the example.
