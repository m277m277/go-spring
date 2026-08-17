# netutil
[English](README.md) | [中文](README_CN.md)

`netutil` 提供 Go-Spring 框架内部使用的网络小工具——当前只有注册与启动日志
用到的本机 IPv4 查询。属于零依赖的 `stdlib` 层，也不是网络框架——接口
枚举、CIDR 匹配、地址解析已由 `net` 覆盖。

## 使用方式

```go
import "go-spring.org/stdlib/netutil"

ip := netutil.LocalIPv4()
```

### API

- `LocalIPv4() string` —— 本机第一个非回环 IPv4 地址，找不到时返回
  `"0.0.0.0"`。首次调用后缓存。

## 关键设计

- 一步回答"我在网络中的地址是什么？"，供服务注册、日志打标、actuator
  info 使用。
- `sync.Once` 缓存让进程周期内结果稳定，与框架"启动后网络地址固定"的假设
  一致；之后接口变化不会被察觉。
- `net.InterfaceAddrs()` 的错误被 `"0.0.0.0"` 占位符吞掉，保持 API 只返回
  字符串——代价是错配置被静默，需要硬失败的调用方应直接使用
  `net.InterfaceAddrs`。
- 仅 IPv4：Go-Spring 面向的注册中心（Nacos / etcd / Consul 风格）与日志
  主流仍以 IPv4 为键。

## License

Apache License 2.0，详见 [LICENSE](../../LICENSE)。
