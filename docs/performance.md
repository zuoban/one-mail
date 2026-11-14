# 可重复性能验证

这些基准用于观察本机回归和分配成本。全部使用临时 SQLite 数据库与合成邮件，不读取运行实例、不连接真实邮箱、不发送 Telegram 消息。

## 运行方式

开发、CI 与 Docker 使用 Go 1.27.4，Node.js 24.22.3/npm 11.19.1。使用 mise 时先信任本仓库并安装工具链：

```bash
mise trust
mise install
mise exec -- make bench
```

工具链已经在 PATH 时可以直接运行 `make bench`。默认每个场景测量 200ms、重复 3 次，并输出 `ns/op`、`B/op` 和 `allocs/op`。延长测量或增加样本：

```bash
make bench BENCHTIME=1s BENCHCOUNT=5
```

后端使用 CGO、SQLite FTS5、WAL 和单连接，保持生产实现的数据库设置。每个邮件顶层基准创建临时数据库，包含 10 个账户、每账户 50 封邮件，共 500 封；列表的四个子场景共用同一份数据。正文约 7.5 KiB。数据准备和建库不计入计时，基准关闭日志输出以降低终端 I/O 干扰。运行时避免同时执行构建或其他负载。

## 场景与边界

| 基准 | 测量内容 | 不包含的工作 |
| --- | --- | --- |
| `BenchmarkMailList/all` | 汇总列表的应用服务查询，最多 50 封，无正文投影 | HTTP、JSON 编码、浏览器 |
| `BenchmarkMailList/account` | 单个账户的 50 封摘要查询 | 远端网络 |
| `BenchmarkMailList/search_like` | 转义后的 LIKE 字面搜索 | FTS5 |
| `BenchmarkMailList/search_fts5` | trigram 字面短语搜索 | 网络和 UI 渲染 |
| `BenchmarkCachedBody` | 应用服务读取已缓存正文，并核对账户身份 | 远端获取 |
| `BenchmarkFirstBodyMock` | 未缓存正文的身份检查、共享工作和正文落库 | IMAP 握手、MIME 传输；读取前复位不计时 |
| `BenchmarkRefreshReconcileParallel` | 10 个账户的未变窗口核对事务；按账户串行、全局最多 4 个工作 | IMAP、完整 `RefreshAccounts` 队列、真实远端延迟 |
| `BenchmarkGzipLargeBody` | 8 MiB 高重复正文的完整响应缓冲与 Gzip 压缩 | 网络传输、HTML 清洗、随机或低可压缩内容 |

`FirstBodyMock` 应持续输出 `1.000 fetches/op`。此处每个迭代主动清空正文缓存；它与共享请求竞态测试分别验证首次获取和重复请求合并。

`RefreshReconcileParallel` 的 `ns/op` 是并发批次总耗时除以完成的事务数，不能当作单请求 P95。使用真实账户评估时还应记录 TLS/登录、UID 核对、缺失元数据请求、正文体积、数据库等待、接口分位延迟和通知积压。

Gzip 池最多保留 256 KiB 的底层缓冲；8 MiB 响应仍会分配相应的临时正文缓冲，因而 `B/op` 约为正文大小是预期行为。这个改动限制请求完成后的池内保留，不是流式传输或进程 RSS 上限。高重复内容的压缩吞吐也不能代表普通邮件。

## 本机样本

2026-11-15，Apple M1、darwin/arm64、Go 1.27.4、默认 8 个逻辑执行槽，运行未调整参数的 `make bench`。下表是三个样本的中位数，耗时和分配各自取中位数，不是请求分位延迟：

| 场景 | 耗时 ms/op | B/op |
| --- | ---: | ---: |
| 汇总列表 | 0.143 | 65,346 |
| 单账户列表 | 0.161 | 65,472 |
| LIKE 搜索 | 0.153 | 73,110 |
| FTS5 搜索 | 0.626 | 65,969 |
| 已缓存正文 | 0.060 | 29,004 |
| 模拟首次正文 | 0.221 | 63,626 |
| 并发未变窗口核对事务 | 0.292 | 108,512 |
| 8 MiB Gzip 响应 | 3.196 | 8,640,954 |

首次正文每个样本均为 `1.000 fetches/op`。在这个仅 500 封、所有主题都匹配的查询中，FTS5 比 LIKE 慢；不能据此认定搜索索引对所有规模都更快或更慢，应使用真实数据量和查询选择性重新测量。样本没有可比的旧版本基线，不计算提升百分比。

## 正确性验证

```bash
cd backend
go test -race -tags fts5 ./...
go vet -tags fts5 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -tags fts5 ./...
cd ../frontend
npm ci
npm test
npm run lint
npm run build
npm audit --audit-level=moderate
cd ..
make test-dev
```

IMAP 测试使用脚本化协议对端，检查只读命令、UIDVALIDITY、未变窗口仅取 UID、仅为新 UID 取元数据和不完整响应保留缓存。共享请求测试覆盖调用者独立取消、全部取消、错误重试、身份隔离和替代请求。HTML 测试检查主题切换复用清洗结果并继续阻止危险内容。

没有前后版本同条件测量时，不计算百分比提升。依赖扫描仅反映运行扫描时的公告状态；Go 及 npm 扫描也持续在 CI 中执行。
