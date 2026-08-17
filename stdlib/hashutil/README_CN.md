# hashutil
[English](README.md) | [中文](README_CN.md)

`hashutil` 是对 `hash/fnv` 的薄封装——Go-Spring 零依赖 `stdlib` 层的单函数
文件，让分片 / 分桶场景免写 `New64a` + `Write` + `Sum64` 三件套。

## 使用方式

```go
import "go-spring.org/stdlib/hashutil"

h := hashutil.FNV1a64("some/key")
```

### API

- `FNV1a64(s string) uint64` —— 使用标准库 `hash/fnv` 实现的字符串 64 位
  FNV-1a 哈希。

FNV-1a 是快速的非密码学哈希，适合做 map 分片、缓存分桶等场景。攻击者可以
控制输入的场景请勿使用。

## 关键设计

- 通过 `hash/fnv` 转发而非手写 FNV-1a 循环——可读性与其它 `hash.Hash`
  用户的一致性优先于几纳秒；无流式 API，分批喂数据直接用 `hash/fnv`。
- 不是密码学哈希包：MD5 独立在 `md5util`；SHA 族与 HMAC 若未来加入也不放
  这里。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
