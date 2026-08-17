# md5util
[English](README.md) | [中文](README_CN.md)

`md5util` 计算字符串的 MD5 摘要，并以小写 hex 字符串返回。属于 Go-Spring
零依赖的 `stdlib` 层。

## 使用方式

```go
import "go-spring.org/stdlib/md5util"

sum := md5util.MD5("hello") // "5d41402abc4b2a76b9719d911017c592"
```

### API

- `MD5(str string) string` —— 小写 hex 编码的 MD5 摘要。

MD5 **不适合**作为密码学认证，仅用于校验和、缓存 key、指纹等允许碰撞的
场景。

## 关键设计

- 一步返回绝大多数场景需要的小写 hex（`encoding/hex.EncodeToString`）
  ——缓存 key、ETag、指纹——符合常见数据库 / 缓存约定。无 HMAC、无流式
  API：分块哈希、密钥派生请直接用 `crypto/md5`（或现代哈希）。
- "一个函数一个包"就是设计意图；SHA-1 / SHA-256 / HMAC 都另开新包、由
  import 显式声明——这也是本包与 `hashutil` 分开的原因。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
