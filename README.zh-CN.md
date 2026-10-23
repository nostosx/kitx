# kitx

[English](README.md) · [API 文档](https://pkg.go.dev/github.com/nostosx/kitx)

kitx 是一组独立的 Go 工具包，提供重试、并发控制、缓存和 I/O 等基础能力。
按需导入，通过 Go 函数和 context 组合使用。代码和测试均仅依赖标准库。

## 安装

需要 **Go 1.27.3 或更高版本**。首个版本 **v1.0.0** 正在准备中。
发布后，在应用模块中安装：

```sh
go get github.com/nostosx/kitx@v1.0.0
```

## 使用

创建一个最多容纳 1,000 个条目的缓存，为每个条目设置过期时间：

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/nostosx/kitx/ttlcache"
)

func main() {
	cache, err := ttlcache.New[string, string](1000)
	if err != nil {
		log.Fatal(err)
	}
	cache.Set("language", "en", time.Minute)
	fmt.Println(cache.Get("language"))
}
```

保存为 `main.go`，运行 `go run .`：

```text
en true
```

缓存满时淘汰最近最少使用的条目，读取不会延长条目的 TTL。

各包可以在调用处组合。例如，将 `retry` 与 `backoff` 配合使用，对指定的读取失败
最多尝试三次，并在指数退避的延迟范围内随机等待：

```go
package example

import (
	"context"
	"time"

	"github.com/nostosx/kitx/backoff"
	"github.com/nostosx/kitx/retry"
)

func ReadWithRetry(
	ctx context.Context,
	read func(context.Context) ([]byte, error),
	isTransient func(error) bool,
) ([]byte, error) {
	delay := backoff.Exponential(100*time.Millisecond, 2*time.Second)
	return retry.Do(ctx, retry.Policy{
		MaxAttempts: 3,
		RetryIf:     isTransient,
		Backoff:     backoff.FullJitter(delay),
	}, read)
}
```

调用方提供读取函数，并决定哪些错误可以重试。通过 `ctx` 的截止时间限制整个操作的时长，
读取函数需要响应取消。只对可以安全重复的操作启用重试。

## 包索引

各包均可通过 `github.com/nostosx/kitx/<package>` 独立导入，使用相同的模块版本。

| 包 | 用途 |
| --- | --- |
| [atomicfile](atomicfile) | 通过临时文件替换目标文件，支持普通路径和 `os.Root`。 |
| [backoff](backoff) | 计算有上限的指数退避延迟，可添加随机抖动。 |
| [boundedio](boundedio) | 在明确的字节上限内读取或复制数据，并检测超限。 |
| [circuitbreaker](circuitbreaker) | 连续失败后拒绝调用，通过探测判断是否恢复。 |
| [cleanup](cleanup) | 按获取顺序的逆序释放资源，并合并错误。 |
| [dataurl](dataurl) | 编解码内联数据 URL，保留 MIME 参数。 |
| [parallel](parallel) | 以有界并发处理切片或迭代器。 |
| [queue](queue) | 通过有界 FIFO 队列协调生产者和消费者。 |
| [ratelimit](ratelimit) | 通过令牌桶控制请求速率。 |
| [retry](retry) | 在次数和时间预算内重试指定错误。 |
| [semaphore](semaphore) | 通过加权许可限制并发任务。 |
| [singleflight](singleflight) | 合并同一键上重叠执行的调用。 |
| [syncmap](syncmap) | 并发访问类型安全的映射，获取浅拷贝快照。 |
| [ttlcache](ttlcache) | 提供 TTL 过期和 LRU 淘汰的内存缓存。 |
| [utf8text](utf8text) | 在包含可选后缀的字节预算内截断文本。 |

各包目录包含可执行的 `Example` 示例；[API 文档](https://pkg.go.dev/github.com/nostosx/kitx)
说明函数签名、错误、零值和并发保证。在源码目录中，也可以通过 `go doc ./retry` 阅读包文档。

## 使用边界

超时、资源上限和授权范围由应用决定：

- **取消：** 回调需要响应 context 取消。`singleflight.Group` 使用首个调用方的 context；
  `SharedGroup` 使用显式指定的所有者 context 和执行超时，并通过 `Close` 取消和等待共享任务。
- **共享值：** 缓存、映射和共享调用不会复制其中的值。修改共享数据时需要同步，或在读写边界复制。
  缓存键和请求键需要包含租户及授权范围。
- **资源限制：** 缓存容量按条目数计算，不代表字节上限；请求合并不限制不同键的数量。
  `dataurl` 不限制输入大小。处理不可信输入时，需要在调用处限制数据大小和并发量。
- **文件替换：** 使用可信父目录或适当的 `os.Root`。原子性遵循 `os.Rename`，Windows 上不保证原子替换。
  写入会同步文件，但不会同步目录；读取后修改再写回的操作需要单独加锁。
  完整约定见 [atomicfile](atomicfile)。

## 参与贡献

环境准备、设计标准和 pull request 要求见 [CONTRIBUTING.md](CONTRIBUTING.md)。
提交变更前运行：

```sh
make check
make lint
make vulncheck
```

[变更记录](CHANGELOG.md) · [发布指南](RELEASING.md) · [安全策略](SECURITY.md)

## 许可证

[MIT](LICENSE)。
