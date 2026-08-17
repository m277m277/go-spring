# Go-Spring :: Log

<div>
   <img src="https://img.shields.io/github/license/go-spring/log" alt="license"/>
   <img src="https://img.shields.io/github/go-mod/go-version/go-spring/log" alt="go-version"/>
   <img src="https://img.shields.io/github/v/release/go-spring/log?include_prereleases" alt="release"/>
   <a href="https://codecov.io/gh/go-spring/log" > 
      <img src="https://codecov.io/gh/go-spring/log/graph/badge.svg?token=QBCHVEK97Q" alt="test-coverage"/> 
   </a>
   <a href="https://deepwiki.com/go-spring/log"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki"></a>
</div>

[English](README.md) | [中文](README_CN.md)

> The project has been officially released, welcome to use!

**Go-Spring :: Log** is a high-performance and extensible structured logging library designed specifically for
the Go programming language. It provides a flexible tag-based classification system, contextual tracing-field
extraction, multi-level logging configuration, and multiple output options, making it ideal for a wide range of
server-side applications.

## Features

* **Multi-Level Logging**: Supports standard log levels such as `Trace`, `Debug`, `Info`, `Warn`, `Error`, `Panic`,
  and `Fatal`, suitable for debugging and monitoring in various scenarios.
* **Structured Logging**: Records logs in a structured key-value format, natively carrying tracing information
  such as `trace_id` and `user_id`, making them easy for log aggregation systems to parse and analyze.
* **Context Integration**: Extracts contextual tracing information (e.g., request ID, user ID) from
  `context.Context` and automatically attaches it to log entries.
* **Tag-Based Logging**: An innovative tag system distinguishes logs across different modules or business lines,
  with hierarchical suffix-wildcard matching, so a unified API is available without explicitly creating logger
  instances.
* **Plugin Architecture**:
    * **Appender**: Supports multiple output targets including console, plain file, and time-rolling file.
    * **Layout**: Provides both plain text and JSON formatting for log output.
    * **Logger**: Offers both synchronous and asynchronous loggers; asynchronous mode does not block the
      business thread.
* **Flexible Rolling Logs**: Rotates files automatically at fixed time intervals, cleans up expired logs, and
  can separate WARN-and-above logs into a dedicated file.
* **Performance Optimizations**: Utilizes buffer pools and log event object pools to minimize memory allocation
  overhead, with excellent benchmark results.
* **Dynamic Configuration Reload**: The logging configuration can be reloaded at runtime via `RefreshConfig`
  without restarting the application; reading configuration files is the caller's job.
* **Well-Tested**: All core modules are covered with unit tests to ensure stability and reliability.

## Core Concepts

### Tag

Tag is the core concept in this logging library, used to categorize logs. After a tag is registered via the
`RegisterTag` function, the configuration can group-match tags with a suffix wildcard (e.g. `_app_request_*`
matches all tags starting with `_app_request_`).

This design enables a unified logging API without explicitly creating logger instances; even third-party
libraries can emit logs in a standardized way. The framework automatically matches each tag to the most
specific logger.

```go
// Register tags by category
var (
	TagAppStartup     = log.RegisterAppTag("startup", "init") // application startup phase
	TagBizOrderCreate = log.RegisterBizTag("order", "create") // order creation business
	TagRpcRedisQuery  = log.RegisterRPCTag("redis", "query")  // Redis query RPC
)
```

### Logger

A `Logger` is the component that actually processes logs. Different tags can be matched to different loggers,
and each logger can independently set its level and output. You can also retrieve a logger instance by name
via `GetLogger`, mainly for compatibility with legacy projects that record pre-formatted messages via `Write`.

### Contextual Field Extraction

You can configure hook functions to extract contextual data from `context.Context` and include it in log entries:

* `StringFromContext`: extracts a string value from the context (e.g., a request ID).
* `FieldsFromContext`: returns a list of structured fields from the context, such as trace ID or user ID.

## Installation

```bash
go get go-spring.org/log
```

## Quick Start

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"go-spring.org/log"
	"go-spring.org/stdlib/flatten"
)

func main() {
	// Configure context field extraction.
	// You may also use StringFromContext if only strings are needed.
	log.FieldsFromContext = func(ctx context.Context) []log.Field {
		return []log.Field{
			log.String("trace_id", "0a882193682db71edd48044db54cae88"),
			log.String("span_id", "50ef0724418c0a66"),
		}
	}

	// Load the logging configuration from a file. Parsing the file is the
	// caller's job: decode it into a map, flatten it, then refresh.
	b, err := os.ReadFile("log.json")
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	if err := log.RefreshConfig(flatten.Flatten(m)); err != nil {
		panic(err)
	}

	ctx := context.Background()

	// Simple formatted logging
	log.Infof(ctx, log.TagAppDef, "app started, version: %s", "v1.0.0")
	log.Errorf(ctx, log.TagBizDef, "failed to handle order request: %v", err)

	// Structured logging
	log.Info(ctx, log.TagAppDef,
		log.String("event", "user_login"),
		log.Int("user_id", 10001),
		log.Msg("user logged in successfully"),
	)
}
```

## Configuration

Go-Spring :: Log supports property files, JSON, YAML, and other configuration formats — the caller parses the
file into a flat property map (e.g., via `stdlib/flatten`) and hands it to `RefreshConfig`. For example:

```properties
# Async logger buffer size
bufferSize=1000

# File appender
appender.file.type=FileAppender
appender.file.dir=./logs
appender.file.file=app.log
appender.file.layout.type=JSONLayout

# Console appender
appender.console.type=ConsoleAppender
appender.console.layout.type=TextLayout

# Root logger
logger.root.type=Logger
logger.root.level=INFO
logger.root.appenderRef.ref=console

# Dedicated async logger for matching tags
logger.request.type=AsyncLogger
logger.request.level=DEBUG
logger.request.tag=_app_request_*,_rpc_*
logger.request.bufferSize=${bufferSize}
logger.request.onBufferFull=block
logger.request.appenderRef[0].ref=file
```

**Configuration notes**:
- `appender.xxx.type` - appender type
- `logger.yyy.type` - logger type
- `logger.yyy.level` - log level range, supports forms like `DEBUG` and `DEBUG~INFO`
- `logger.yyy.tag` - list of tags to match, supports the suffix wildcard
- `${property}` variable references are supported

## Built-in Plugins

### Appender

| Plugin | Description |
|--------|-------------|
| `ConsoleAppender` | Writes to standard output |
| `FileAppender` | Writes to a single file |
| `RollingFileAppender` | Rotates files at fixed time intervals, automatically cleans up expired logs |
| `DiscardAppender` | Discards all logs |

### Layout

| Plugin | Description |
|--------|-------------|
| `TextLayout` | Human-readable plain text format |
| `JSONLayout` | Structured JSON format |

### Logger

| Plugin | Description |
|--------|-------------|
| `Logger` / `SyncLogger` | Synchronous logger that writes on the calling thread |
| `AsyncLogger` | Asynchronous logger processed by a background thread without blocking the business logic. Supports three buffer-full strategies: `block` (wait for space), `discard` (drop the new event), `drop-oldest` (drop the oldest event) |
| `ConsoleLogger` | Convenience logger that writes directly to the console |
| `FileLogger` | Convenience logger that writes directly to a file |
| `RollingFileLogger` | Convenience logger for time-rolling files, supports error-log separation |
| `DiscardLogger` | Discards all logs |

**RollingFileLogger features**:
- Rotates log files automatically at the configured time interval
- Automatically removes old logs beyond the maximum retention period
- Supports `separate=true` to split WARN-and-above logs into a dedicated `.wf` file, making troubleshooting easier

## Benchmarks

The project ships benchmarks against mainstream logging libraries (zap, logrus, zerolog, slog, etc.); this
library delivers excellent performance while keeping a concise and extensible API. Run the following command
to see the comparison:

```bash
go test -bench=. ./benchmarks/logs
```

## Custom Extensions

You can extend the library by implementing the following interfaces:

- `Appender` interface: custom output targets
- `Layout` interface: custom output formats
- Register the implementations via `RegisterPlugin`, then use them in the configuration

## License

Go-Spring :: Log is licensed under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).
