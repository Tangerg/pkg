# CLAUDE.md — pkg

> 独立的 Go 通用工具库：generics 集合、并发原语、流式处理、JSON Schema 生成等。pkg 不依赖任何业务模块，这是 zero-cycle 的关键护栏。
> 子包名录与依赖版本以代码为准，本则只讲宏观。

---

## 定位

- **纯工具,零业务**:pkg 是依赖 DAG 的底,只放跨业务模块可复用的通用原语,不 import 应用或业务模块。
- **独立公开模块**:module path 是 `github.com/Tangerg/pkg`，exported API 改动尤其要慎(见下)。

## 架构心智

- **按职能分组、每子包一个 niche**:集合 / 数据类型 / 并发 / 流式 / 编码 / 基础原语。彼此独立,消费方只拉自己用到的。
- **generics 强制**:集合与通用 API 用类型参数,公开 API 禁 `any` / `interface{}`。
- **iterator-first**:优先 range-over-func 而非 `ForEach(func(T))` —— 调用方拿到 break / 提前退出。
- **流式不缓冲**:增量处理优先;处理不可信输入(LLM 输出、XML / JSON)强制 buffer 上限,防 OOM。
- **小到不必抽的直接放**:极小 helper 不二次封装 stdlib。
- **零消费者的子包是有意的工具箱,不是死代码**:pkg 是通用 toolkit,某些导出暂无消费方是备用,不在 dead-code 清扫里删。

## 模块特有反向不变量

- ❌ **import 任何业务模块** —— 会形成循环,CI 应锁这条。
- ❌ **加业务概念** —— 只放纯工具;带业务语义的分类(如 retry 的 Transient / NonTransient)已被否(见 root 共用反向不变量)。

## 改动前必看(波及面)

- **加新子包**:先问"stdlib 为什么不够" —— 只在 stdlib 真不够或跨业务模块要复用时才加。
- **改 exported API**:宁可加新函数也别改老签名;**任何破坏性改动先咨询 scope + 影响面**。
- **改 XML / JSON parser 的 buffer 上限**:跑 fuzz,覆盖恶意 LLM 输出。

## 非显然决策与易错点（记忆）

> 逐条来自实际改动;违反往往不被现有测试立刻发现,故固化于此。

- **maps 值比较只有一个入口**:`HashMap` / `LinkedMap` / `SyncMap` / `StdSyncMap` 的含值比较(`ContainsValue`、`RemoveIf`、`ReplaceIf`、`Compute` 族、`ReplaceAll`)一律走 `maps.valuesEqual`(可比较动态类型用 `==`,否则 `reflect.DeepEqual`)。禁止新增裸 `reflect.DeepEqual`:不可比较值会变慢,而 `sync.Map` 的 `CompareAndSwap` / `CompareAndDelete` 对 nil 接口或不可比较值会 panic。`StdSyncMap` 另用 `unwrap[T]`(comma-ok)避开 nil 接口断言 panic。
- **并发 map 回调不持锁**:`SyncMap` / `StdSyncMap` 的 `ForEach`、`ReplaceAll`、`Compute` 族在**无锁**下调用用户函数,允许回调重入同一 map;`StdSyncMap` 走 CAS 重试,回调可能被调用多次,必须是参数的纯函数。`PutAll` 先对 source 取快照再上写锁,避免两个 `SyncMap` 互拷的锁序反转。
- **xml 流式上限不可旁路**:`StreamScanner` 内部所有写入必须经 `appendScope` / `appendText` / `writeTagByte` 等受控 helper;元素上限(`ElementListener.MaxBufferSize`)、元素外文本上限(`MaxTextBufferSize`)、tag 暂存上限三处都要生效。动这些路径必须保留 `xml/scanner_test.go` 里的 `*CapCovers*` 回归测试。
- **json 流式计费**:`stream_parser.go` 的 `buffered` 对同一 top-level 值跨作用域只计一次,值完成 / 丢弃后归零;所有解析器错误必须经 `fail` 恰好上报 `OnError` 一次,`OnError` 不接收用户回调或 reader 的错误。
- **text.Lines 语义**:对齐 `bufio.ScanLines`(去尾部 `\r`、无尾随空行),但**无 token 上限**——长行不截断。不要再改回 `bufio.Scanner`(其 64 KiB 上限会静默丢行)。
- **retry 抖动的唯一随机源**:`RandomJitter` / `FullJitterBackoff` 只能经包内 `randInt64N` 变量取随机数——`math/rand/v2` 没有可播种的全局源,测试只能靠替换它拿到确定性。不要改回直接调 `rand.Int64N`。
- **retry 的饱和算术只有一个入口**:`ExponentialBackoff` / `FullJitterBackoff` 的 `BaseDelay << step` 一律走 `shiftSaturating`,`CombineDelays` 的累加走 `addSaturating`。**不要退回 `d < 0` 那种判溢出**:移位回绕成 0 或小正数时它不是负数(`BaseDelay = 1<<62`、`attempt = 2` 时 `1<<64 ≡ 0`),会静默算出 0 延迟——即无间隔重试的死循环。`CombineDelays` 同理不能拿 `MaxInt64 - d` 当上界:分量可以为负,那个减法自身就会回绕,把 -1s 这类偏移放大成一个 292 年的 sleep。
- **`MaxBackoffStep == 0` 只有一个含义**:uncapped,由 `cappedStep` 单点决定,`NewResultRetrier` 不再改写该字段。历史上 0 同时表示"自动派生"与"派生结果无指数可用",两个 delay 函数又拿 `MaxBackoffStep > 0` 当封顶开关,于是退化输入下反而把保护关掉了——这正是回绕能漏出去的原因。溢出改由饱和算术负责,不再需要派生上限。
- **retry 测试不得并行**:`retry` 包用替换 `randInt64N` 的方式做确定性断言,`t.Parallel` 会与替换互相干扰。
- **mime 的通配类型与引号各只有一个归属**:通配主类型只允许 `*/*`,`Builder.Build` 是唯一校验点(`Parse` 经它校验)——否则 `New("")` / `WithSubType` 能组合出 `*/html` 这类 `String()` 回不去 `Parse` 的值,且 `Includes` 会把它当成覆盖一切。引号只认双引号(`isQuotedSpelling`),单引号是合法 token 字符:别把 `normalizeTypeComponent` / `normalizeParamKey` 改回 `strings.IsQuoted`,它连单引号一起剥,会静默改写合法 token。`RegisterXSubtype` / `RegisterXSubtypes` 返回 error 并前置校验(键须带 `x-` 前缀、目标须是非空 token),因为映射目标会被替换进 subtype——不校验就能造出 `text/a b` 这种解析不回去的值。

## 改动后必跑

- `gofmt -l .`、`go vet ./...`、`go test -race ./...`(并发包尤其)。
- `./scripts/check-imports.sh`(依赖闭包护栏:只允许 stdlib 与 go.mod 声明的三方库,且不 import 业务模块)。
- 改 `xml` / `json` 解析器:跑对应 `-fuzz` 目标各数秒。
- 改 `mime` 的 `Parse` / 引号 / 参数路径:跑 `go test -run=XXX -fuzz=FuzzParse -fuzztime=15s ./mime/` 与 `go test -run=XXX -fuzz=FuzzBuilder -fuzztime=15s ./mime/`(`mime/testdata/fuzz` 里的历史 crasher 会随 `go test` 一起回归)。
