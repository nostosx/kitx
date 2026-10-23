# kitx v1.0.0

## English

The first stable release of kitx includes 15 independent Go packages for
retries, concurrency, caching, and I/O. Code and tests depend only on the
standard library. Import the packages you need and compose them with functions
and contexts.

### Install

Requires Go 1.27.3 or later. Run this command from your application module:

```sh
go get github.com/nostosx/kitx@v1.0.0
```

### Included in v1.0.0

- Retry selected failures with attempt and time budgets, capped exponential
  backoff, and full jitter.
- Bound parallel work, share overlapping calls for the same key, and coordinate
  work through weighted permits, a token bucket, and a bounded FIFO queue.
- Cache entries with individual TTLs and LRU eviction, access typed maps
  concurrently, and stop calls with a circuit breaker while probing recovery.
- Replace files through temporary files, bound stream reads and copies, and
  release resources in reverse order.
- Encode and decode data URLs with MIME parameters, and truncate UTF-8 text
  within a byte budget that includes the suffix.

The v1 API is the compatibility baseline. All packages share the module version;
patch and minor releases preserve documented APIs and behavior.

Applications own deadlines, resource limits, authorization scopes, and
synchronization of shared mutable values. Callbacks must honor cancellation.
File replacement follows `os.Rename`: Windows atomicity and directory durability
are not guaranteed. See the
[usage boundaries](https://github.com/nostosx/kitx/blob/v1.0.0/README.md#usage-boundaries) and
package documentation for the full contracts.

Start with the [README examples](https://github.com/nostosx/kitx/blob/v1.0.0/README.md#usage), browse the
[package index](https://github.com/nostosx/kitx/blob/v1.0.0/README.md#packages), or read the
[v1.0.0 API reference](https://pkg.go.dev/github.com/nostosx/kitx@v1.0.0).
The [changelog](https://github.com/nostosx/kitx/blob/v1.0.0/CHANGELOG.md) lists the packages in this release.
kitx uses the [MIT License](https://github.com/nostosx/kitx/blob/v1.0.0/LICENSE).

## 简体中文

kitx 首个稳定版本包含 15 个独立的 Go 包，覆盖重试、并发控制、缓存和 I/O。
代码和测试均仅依赖标准库，按需导入，通过函数和 context 组合使用。

### 安装

需要 Go 1.27.3 或更高版本。在应用模块中运行：

```sh
go get github.com/nostosx/kitx@v1.0.0
```

### v1.0.0 包含的能力

- 在次数和时间预算内重试指定错误，支持有上限的指数退避和随机抖动。
- 限制并行任务数量，合并同一键上重叠执行的调用，通过加权许可、令牌桶和有界 FIFO 队列协调任务。
- 通过独立 TTL 和 LRU 淘汰管理缓存，并发访问类型安全的映射，通过熔断器拒绝调用并探测恢复。
- 通过临时文件替换目标文件，限制流读取和复制的字节数，按获取顺序的逆序释放资源。
- 编解码保留 MIME 参数的数据 URL，在包含后缀的字节预算内截断 UTF-8 文本。

v1 API 是兼容性基线。各包使用相同的模块版本，补丁版本和次版本保持已文档化的 API 与行为兼容。

超时、资源上限、授权范围和共享可变值的同步由应用负责，回调需要响应取消。
文件替换遵循 `os.Rename`，不保证 Windows 上的原子性，也不保证目录持久性。
完整约定见 [使用边界](https://github.com/nostosx/kitx/blob/v1.0.0/README.zh-CN.md#使用边界) 和各包文档。

从 [README 示例](https://github.com/nostosx/kitx/blob/v1.0.0/README.zh-CN.md#使用) 开始，也可以查看
[包索引](https://github.com/nostosx/kitx/blob/v1.0.0/README.zh-CN.md#包索引) 和
[v1.0.0 API 文档](https://pkg.go.dev/github.com/nostosx/kitx@v1.0.0)。
[变更记录](https://github.com/nostosx/kitx/blob/v1.0.0/CHANGELOG.md) 列出了本次发布的各个包。
kitx 使用 [MIT 许可证](https://github.com/nostosx/kitx/blob/v1.0.0/LICENSE)。
