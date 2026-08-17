# ctxcache

[English](README.md) | [中文](README_CN.md)

`ctxcache` is a strongly-typed, context-scoped cache for request-scoped data,
part of the zero-dependency `stdlib` layer. It attaches a concurrency-safe,
write-once key-value store to a `context.Context`, letting values propagate
across call boundaries without polluting function signatures. It is not a
general-purpose cache — no TTL, eviction, or hit rates — just a small,
guaranteed-scoped, guaranteed-typed store whose lifecycle is exactly the
enclosing request. Typical uses: authenticated user objects, permission lists,
trace metadata, and computed intermediates shared across a call chain.

## Usage

Initialize the cache at the request boundary (e.g. in HTTP middleware) and
clean it up on exit:

```go
ctx, cancel := ctxcache.Init(ctx)
defer cancel()
```

Set a value. Each key can only be set once; repeated attempts return
`ErrKeyAlreadySet`:

```go
err := ctxcache.Set(ctx, "user", userInfo)
if err != nil {
    // handle error
}
```

Retrieve it downstream. `Get` is generic; the type parameter is what makes the
lookup type-safe:

```go
value, err := ctxcache.Get[UserType](ctx, "user")
if err != nil {
    // handle error
}
```

Calling the cancel function returned by `Init` clears all cached values and
makes the cache permanently unusable — subsequent `Get` or `Set` return
`ErrCacheAlreadyCleared`:

```go
cancel()
```

### API

- `Init(ctx) (context.Context, func())` — attach a cache and get its cancel
  function. Idempotent: a repeated call on the same context returns the original
  context and a no-op cancel.
- `Set[T](ctx, key, value) error` — assign a value; once per key.
- `Get[T](ctx, key) (T, error)` — retrieve a value by typed key.
- `Cache.Clear()` — remove all values and mark the cache cleared (idempotent).

Errors returned by the package (all testable with `errors.Is`):

- `ErrCacheNotInitialized`: cache is not initialized
- `ErrCacheAlreadyCleared`: cache has already been cleared
- `ErrKeyNotSet`: key is not set
- `ErrKeyAlreadySet`: key is already set

A complete example:

```go
package main

import (
    "context"
    "fmt"
    "go-spring.org/stdlib/ctxcache"
)

type User struct {
    ID   int
    Name string
}

func main() {
    ctx := context.Background()

    // Initialize cache
    ctx, cancel := ctxcache.Init(ctx)
    defer cancel()

    // Set user info
    user := User{ID: 1, Name: "Alice"}
    if err := ctxcache.Set(ctx, "user", user); err != nil {
        panic(err)
    }

    // Get user info in downstream code
    retrievedUser, err := ctxcache.Get[User](ctx, "user")
    if err != nil {
        panic(err)
    }

    fmt.Printf("User: %+v\n", retrievedUser)
}
```

## Design

The package owns one pattern: "one request, one bag of typed values, cleared at
the boundary". Everything is anchored to a specific context; there is no
process-global map.

- **`Cache`**: a mutex-protected `map[any]any` attached to the context via a
  private key. Exactly one `Cache` per context; a second `Init` on the same
  context is a no-op.
- **`TypedKey[T]`**: keys are `(string, type)` pairs produced by generics. The
  same name under two different `T`s is two disjoint slots — which is why
  `Get`/`Set` are generic instead of taking `any`.
- **Explicit lifecycle**: `Init` returns a cancel function that clears the map
  and permanently marks the cache cleared. There is deliberately no "second
  life" — reuse would silently share state across requests.
- **Write-once per key** makes lookups a total contract: a value that is
  present cannot mutate under the caller. Callers that want mutability must
  store a pointer or a `sync.Map` under a stable key.
- **The cancel function must be called**, usually via `defer` at the middleware
  boundary. A missing call leaks the (small) map but no goroutines.
- **Concurrency**: a single mutex is the simplest correct choice; contention is
  unlikely because request-scoped data usually flows sequentially.

## License

`ctxcache` is distributed under the Apache License 2.0. See
[LICENSE](../../LICENSE) for details.
