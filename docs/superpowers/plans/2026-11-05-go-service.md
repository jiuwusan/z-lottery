# Go Service Implementation Plan

> **For agentic workers:** Use parallel agents for independent domain/provider tasks; root integrates repository, HTTP, scheduler and deployment. Work on current branch, without commits or pushes.

**Goal:** Go 后端兼容旧数据，内部调度按彩种与窗口同步官方完整开奖，再从持久缓存进行本地验奖。
**Status:** 第一阶段迁移验证记录仅对应更正前实现；官方同步停止条件更正阶段仍待根任务实现与验证，不据此宣称当前工作完成。
**Architecture:** `internal/lottery` 包提供模型、领域算法、官方数据适配、SQLite 仓储、服务与调度；`cmd/z-lottery` 启动服务，静态文件嵌入二进制。
**Tech Stack:** Go、net/http、modernc SQLite、robfig cron；原生浏览器前端继续保留。
**Spec:** `docs/superpowers/specs/2026-11-05-go-service-design.md`

## Global Constraints

- 当前分支，沿用 SQLite JSON 与端口/数据卷；不自动提交推送或部署。
- 北京时间 22:00 至次日 00:00，每 20 分钟；包含 00:00，不包含 00:20。
- 上游停止条件是官方列表最新开奖已审核且全部官方奖级金额完整，与投注 `winAmount` 或未来待匹配期数无关。
- 官方列表是唯一依据，最新记录日期早于窗口也可完成同步；非开奖日不等待不存在的今日或下一期开奖。列表无记录、最新未审核或缺少官方奖级金额时继续重试。
- 双色球一至六等奖，大乐透现行一至七等奖、历史规则一至九等奖及一、二等奖追加金额均需完整。
- 按彩种与窗口持久化完成状态，午夜属于前一天窗口；同窗口重启复用完成状态，下一窗口重新发现，不预推期号或硬编码开奖日历。
- 零投注仍同步；本地匹配复用开奖缓存，必要历史回填覆盖范围独立持久保存；启动补查、防重叠、退出取消。
- 不暴露 HTTP 验奖触发端点。

## Review Focus

- 旧数据日期/明确期号模式与跨年结果保留；仓储迁移测试覆盖。
- 任一官方奖级或追加奖金缺失都不能完成上游同步，包括本次投注未命中的奖级；领域和服务测试覆盖。
- 午夜边界、不同主机时区不能改变调度；调度 next 测试覆盖。
- 上游失败、并发删除、重复检查不得破坏事务；服务测试覆盖。
- 停止服务取消任务后再关数据库，旧外链全部移除；调度和 HTTP 测试覆盖。

## Task 1: Domain and date matching

Files: `internal/lottery/model.go`, `domain.go`, `matching.go`, corresponding tests.
Interfaces: `NormalizeBet([]byte,time.Time)(Payload,error)`, `NormalizeIssue(string,string)(string,error)`, `Evaluate(Bet,Draw)(Result,error)`, `SelectDraws(string,string,int,[]Draw)([]Draw,error)`, `ShanghaiDate(time.Time) string`, `SalesCloseAt(Draw)(string,error)`.
- [x] Write and run failing Go tests for old domain scenarios and deadlines.
- [x] Port validation, combination accounting, JS-safe precision guards and date matching.
- [x] Pass domain tests.

## Task 2: Official provider

Files: `internal/lottery/provider.go`, `provider_test.go`.
Interfaces: `Provider` with `GetDraw(context.Context,string,string)(*Draw,error)`, `GetHistory(context.Context,string,string,string)([]Draw,error)`; `NewDrawProvider(*http.Client) *DrawProvider`.
- [x] Write failing tests with local HTTP transport fixtures.
- [x] Port request limits, parsing, history paging/coverage/gap checks and context cancellation.
- [x] Pass provider tests.

## Task 3: SQLite, service and HTTP

Files: `repository.go`, `service.go`, `http.go` and tests under `internal/lottery`; `lottery/embed.go`.
Interfaces: `OpenRepository(string)(*Repository,error)`; methods Create/Get/List/Remove/Pending/SaveDateCheck/SaveCheck; `Service.Check(ctx,after,limit)` batch API remains internal.
- [x] Write failing persistence/legacy schema and initial service tests. Original bet-based upstream stopping tests are superseded by Task 6.
- [x] Port repository SQL, transactions and API JSON compatibility; single DB connection and serialized repository operations.
- [x] Serve embedded allowlisted static files, retain API validation/limits, return 404 for old job URL.
- [x] Verify old database, pagination, partial retry, error isolation, delete and HTTP round trips.

## Task 4: Scheduler and process

Files: `scheduler.go`, `scheduler_test.go`, `cmd/z-lottery/main.go`.
- [x] Assert default next times 22:00, 22:20, 23:40, 00:00, next 22:00; initial local complete vs partial selection. Upstream stopping rules are superseded by Task 6.
- [x] Implement configured timezone and semicolon-separated five-field cron schedules; default `*/20 22-23 * * *;0 0 * * *`.
- [x] Direct invocation traverses all pending pages; atomic non-overlap, startup check, cancellation and logs.
- [x] Start HTTP and scheduler, cancel/join before closing DB, provide binary healthcheck command.

## Task 5: Frontend and deployment migration

Files: Dockerfile, Compose, environment example, CI, package.json, README and service/deployment docs, frontend records/index/style.
- [x] Replace external link with automatic checking help; delete legacy Node backend after Go tests cover behavior.
- [x] Build Go binary and use non-root multi-stage image with certificates/timezone; retain volume and port.
- [x] Update CI for Go tests/race/vet and persistent-volume smoke test; keep only standalone JS selection tests.
- [x] Run Go tests/vet/race where supported, JS tests, Linux cross-build, browser smoke and available Docker checks; report limitations.

## Task 6: 官方开奖窗口同步更正（已验证）

Files: provider/model/repository/service/scheduler and corresponding tests; user/deployment docs and frontend help.
- [x] Separate official synchronization from local bet matching; poll latest official records even with zero bets.
- [x] Require the official list latest draw to exist, be reviewed and have complete official prize grades, regardless of its date. A complete prior-date latest draw completes non-draw-day windows; absent/unreviewed latest records and missing non-winning/append grades keep retries active.
- [x] Persist per-game/per-window completion in `draw_sync`; midnight belongs to previous date, same-window restart skips completed requests, next window reopens discovery.
- [x] Reuse cached draws for future-period bets; future unannounced periods never extend a completed window's HTTP polling.
- [x] Backfill missing historical coverage once as needed and persist the range independently of current-window completion, including legacy issue bets.
- [x] Verify fake upstream request counts across retries, midnight, restarts and next windows; cover no bets, unreviewed latest draw, complete prior-date latest draw on non-draw days, next-window discovery without hypothetical new issues, all official grades and per-game isolation.
- [x] Run root integration tests, vet, JS tests, cross-build and independent review; report race/Docker environment limits.
- [x] Confirm user docs and UI help describe official-window completion consistently; root marks this phase complete only after verification.

## 第一阶段验证记录（不覆盖 Task 6）

- Go 全套测试、go vet、JS 选号测试、浏览器新增/列表/删除及旧触发路径 404 均通过。
- SQLite 旧表结构自动补列、日期记录重启持久化、事务回滚、同一期单条失败隔离与本地已完成投注跳过已覆盖；原上游停止判断已被 Task 6 更正，不代表新同步规则通过验证。
- Linux amd64 与 arm64 纯 Go 交叉编译通过；本地二进制健康检查通过。
- 本机未安装 Docker、CGO 关闭且无 C 编译器；Docker Compose/容器持久卷和 race 检查已配置到 Linux CI，本地无法执行。
- 第一阶段独立审查已完成；Task 6 的更正验证与复审结果见下方记录。未提交、推送或部署。

## 官方列表停止条件更正验证

- Go 全套测试与 go vet 通过；JS 测试通过。
- 已覆盖：零投注同步、金额完整后跨轮次/重启不再请求、午夜同窗口和下一窗口发现、非开奖日不等待假想期号、41条未来100期投注跨页不驱动HTTP、非命中奖级缺失继续同步、历史覆盖单次回填、官方首页范围外记录完整性、同一期冲突拒绝和事务回滚。
- 官方同步状态独立于投注状态持久化；本地投注匹配只读取缓存。
- 独立审查提出的首页范围外同一期数据冲突问题已通过失败测试复现并修复，复审通过，无未处理问题。Docker与race检查仍受本机环境限制，保留Linux CI验证。
- 未提交、推送或部署。
