# pkg

`github.com/Tangerg/pkg` 是一个独立的 Go 通用工具库：泛型集合、并发原语、流式处理、编码与解析、
基础原语。它位于依赖图的最底层，只放跨模块复用的纯工具，**不 import 任何业务模块**
（zero-cycle），因此可以在任意位置安全依赖。

- Module path：`github.com/Tangerg/pkg`；协议：[Apache-2.0](LICENSE)。
- 面向不可信输入的解析器（`xml`、`json`）强制 buffer 上限与嵌套深度上限，默认对 LLM 输出、
  网络输入安全；负值才会关闭上限，仅适用于可信输入。
- 集合与通用 API 一律使用类型参数。公开 API 不出现裸 `any`，反射式 API 除外
  （`json` 的 schema 生成、`math.IsNumericType`、`safe.NewPanicError`）。
- 迭代优先 `range` over func：`Iter()` 返回迭代器，调用方可以 `break` 提前退出。
- 没有消费方的子包是有意保留的工具箱，不是死代码。

## 子包名录

| 子包 | 用途 |
| --- | --- |
| `assert` | panic 式断言（`Must`、`Ensure`），用于初始化路径 |
| `bufio` | `bufio.Scanner` 的补充切分函数（`ScanLinesAllFormats`） |
| `dataunit` | 强类型字节单位：`DataSize`、单位换算与解析 |
| `io` | `io.Reader` 扩展：一次读取与迭代式分块读取 |
| `json` | JSON Schema 生成，以及带上限的流式解析器 |
| `maps` | `Map` 一族实现：`HashMap`、`LinkedMap`、`SyncMap`、`StdSyncMap` |
| `math` | 泛型数值助手：绝对值、精确乘除、切片数值转换 |
| `mime` | MIME 类型解析、比较、归一化与内容探测 |
| `ptr` | 指针取址、取值与克隆 |
| `random` | `math/rand/v2` 之上的范围随机 |
| `result` | 泛型 `Result[T]`，用于把 `(T, error)` 串成链 |
| `retry` | 可配置重试：退避、抖动、重试条件与上下文取消 |
| `safe` | goroutine 与函数级的 panic 恢复 |
| `sets` | `Set` 一族实现（`HashSet`、`LinkedSet`、`SyncSet`）与集合运算 |
| `slices` | 泛型切片助手：`Map`、`Chunk`、`At`、`First`/`Last` 族 |
| `stream` | 泛型、带 context 的流式原语：`Map`、`Filter`、`FlatMap`、`Tee`、`Multi` |
| `strings` | `strings` 的补充：引号识别与去引号 |
| `sync` | 并发原语：`Future[V]`、`Limiter`、可插拔 `Pool` |
| `system` | 平台相关的 `LineSeparator` |
| `text` | 行处理与泛型模板渲染 |
| `xml` | 面向 LLM 输出的流式 XML 元素扫描器 |

> `sync` 与标准库同名：同时使用时需要起别名，例如 `pkgsync "github.com/Tangerg/pkg/sync"`。
> `json` 同理（与 `encoding/json` 并存时），下面的示例写作 `pkgjson`。

## 快速上手

### 集合与迭代

```go
m := maps.NewLinkedMap[string, int]()
m.Put("a", 1)
m.Put("b", 2)
for k, v := range m.Iter() {
	fmt.Println(k, v) // a 1，然后 b 2：LinkedMap 保持插入顺序
}

s := sets.Of(1, 2, 3, 3)
fmt.Println(s.Size()) // 3，元素自动去重
fmt.Println(sets.Intersection(sets.Of(1, 2), sets.Of(2, 3)).ToSlice()) // [2]

fmt.Println(slices.Chunk([]int{1, 2, 3, 4, 5}, 2)) // [[1 2] [3 4] [5]]
```

`Set` 与 `Map` 都是接口，`Clone` 返回独立副本，`ToSlice` / `Keys` / `Values` 返回的切片
不与原容器共享底层数组。需要并发访问时用 `maps.NewSyncMap` / `maps.NewStdSyncMap` /
`sets.NewSyncSet`，它们的回调在无锁状态下调用，允许重入。

### 重试

```go
attempts := 0
err := retry.Do(
	func() error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	},
	retry.WithMaxAttempts(5),
	retry.WithBaseDelay(5*time.Millisecond),
	retry.WithMaxDelay(time.Second),
	retry.WithExponentialBackoff(),
	retry.WithOnRetry(func(attempt int, err error) {
		fmt.Printf("attempt %d failed: %v\n", attempt, err)
	}),
)
fmt.Println(err, attempts) // <nil> 3
```

退避策略通过 `Option` 组合：`WithFixedDelay`（恒定）、`WithExponentialBackoff`（指数 + 抖动，
默认）、`WithFullJitter`（AWS 风格全抖动），配合 `WithBackoffStep` 限制指数、
`WithMaxDelay` 限制单次上限、`WithMaxJitter` 控制抖动幅度。指数计算在溢出时饱和到
`math.MaxInt64` 而不是回绕，`MaxBackoffStep` 为 0 表示不限制指数。
`WithSleep` 可替换等待实现，便于测试；`WithContext` 让整个重试受上下文约束。

### 并发原语

```go
f := pkgsync.NewFutureTask(func(interrupt <-chan struct{}) (string, error) {
	return "done", nil
})
f.Run()
v, err := f.Get()
fmt.Println(v, err) // done <nil>

pool := pkgsync.DefaultPool()
_ = pool.Submit(func() { fmt.Println("task ran") })

limiter := pkgsync.NewLimiter(4)
limiter.Acquire()
defer limiter.Release()
```

`Future[V]` 支持 `GetWithTimeout`、`GetWithContext`、`TryGet`、`Cancel(mayInterruptIfRunning)`
与状态查询。`Pool` 是可插拔的提交接口：默认实现（`PoolOfNoPool`）为每个任务起一个
可恢复 panic 的 goroutine，不限制并发、`Submit` 不阻塞；可用 `PoolOfConc`、
`PoolOfAnts`、`PoolOfWorkerpool` 适配已有线程池，再用 `SetDefaultPool` 全局替换。

### 流式处理

```go
ctx := context.Background()
src := stream.OfSliceReader([]int{1, 2, 3, 4, 5, 6})
doubled := stream.Map(
	stream.Filter(src, func(v int) bool { return v%2 == 0 }),
	func(v int) int { return v * 2 },
)
for {
	v, err := doubled.Read(ctx)
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return err
	}
	fmt.Println(v) // 4 8 12
}
```

`Reader` / `Writer` / `Stream` 都是泛型接口，`Pipe` 返回相连的读写两端，
`TeeReader`、`MultiReader`、`MultiWriter`、`Distinct`、`FlatMap` 用于组装管道。
每次读写都带 `context`，写入已关闭的流返回 `stream.ErrStreamClosed`。

### 解析不可信输入

```go
scanner, err := xml.NewStreamScanner(xml.StreamScannerConfig{
	Listeners: []*xml.ElementListener{{
		Name:          xml.Name{Local: "item"},
		MaxBufferSize: 1 << 20, // 单个元素缓冲上限，超出即丢弃或报错
		OnComplete: func(el xml.Element) error {
			fmt.Println(el.String())
			return nil
		},
	}},
	MaxTextBufferSize: 1 << 20, // 元素外文本上限
	StrictMode:        true,    // 结构错误直接终止
	OnError:           func(err error) { fmt.Println("xml error:", err) },
})
if err != nil {
	return err
}
if err := scanner.Scan(strings.NewReader(`<root><item id="1">hello</item></root>`)); err != nil {
	return err
}
// <item id="1">hello</item>
```

```go
parser, err := pkgjson.NewStreamParser(pkgjson.StreamParserConfig{
	Reader:        strings.NewReader(`{"a":1}{"b":[1,2]}`),
	MaxBufferSize: 1 << 20, // 同一 top-level 值的缓冲上限
	OnObject: func(obj map[string]any) error {
		fmt.Println(obj)
		return nil
	},
})
if err != nil {
	return err
}
if err := parser.Parse(); err != nil {
	return err
}
// map[a:1]
// map[b:[1 2]]
```

两个解析器都按 top-level 值增量回调，不会把整个输入读进内存。JSON 解析器另有
`MaxDepth`（默认 10000）与 `OnArray` / `OnValue` / `OnError` 钩子；`OnError` 恰好收到
一次解析错误，且不会透传用户回调或 reader 自身的错误。

### MIME 与其它原语

```go
mt, err := mime.Parse("text/html; charset=utf-8")
fmt.Println(mt.TypeAndSubType(), mt.Charset(), err) // text/html UTF-8 <nil>

pdf, err := mime.Parse("application/pdf")
fmt.Println(mime.IsApplication(pdf), err) // true <nil>
```

```go
size, err := dataunit.SizeOfMB(3)
fmt.Println(size.KB(), err) // 3072 <nil>

unit, err := dataunit.NewUnitFromSuffix("GB")
fmt.Println(unit.Size().B(), err) // 1073741824 <nil>

out, err := text.Render("Hello {{.Name}}", struct{ Name string }{"World"})
fmt.Println(out, err) // Hello World <nil>

r := result.New(strconv.Atoi("42"))
fmt.Println(result.Map(r, func(n int) int { return n * 2 }).Value()) // 84

safe.WithRecover(func() {
	panic("boom")
}, func(err error) {
	fmt.Println("recovered:", err) // 带时间戳与堆栈的 *safe.PanicError
})()
```

## 依赖与反向不变量

pkg 只依赖标准库和 `go.mod` 中声明的三方库，且不依赖任何业务模块——这条不变量由
`scripts/check-imports.sh` 用 `go list` 依赖闭包断言，并在 CI 中执行：

```sh
./scripts/check-imports.sh
```

## 开发

```sh
gofmt -l .          # 必须无输出
go vet ./...        # 必须无输出
go test -race ./... # 全绿；并发子包尤其依赖 -race
```

CI（`.github/workflows/ci.yml`）在每次 push 到 `main` 与每个 PR 上重复上述检查，
并额外跑一次依赖闭包断言。改动 `xml` / `json` 解析器时，另外跑对应的 fuzz 目标：

```sh
go test -fuzz=FuzzStreamScanner -fuzztime=30s ./xml
go test -fuzz=FuzzStreamParser -fuzztime=30s ./json
```
