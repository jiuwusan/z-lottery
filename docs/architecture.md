# 项目架构

本项目是 Go HTTP 服务、离线选号 CLI 和嵌入式浏览器页面，继续使用原生 net/http 和 SQLite，无需迁移数据。

## 目录与职责

- cmd/z-lottery：环境校验、SQLite/开奖网关/验奖服务/调度器/HTTP 组装，处理信号与关闭；healthcheck 子命令只请求本机健康接口。
- cmd/lottery：离线命令行解析和展示，只依赖 domain 的选号规则。
- internal/domain：投注/开奖/结果 DTO，输入规范化、中奖计算、日期匹配、随机选号及官方奖金完整性；不读取磁盘、环境变量或请求上游。crypto/rand 是选号熵源这一明确例外，不承担外部协议。
- internal/lottery：投注管理与验奖用例、官方发布窗口/同步、批次游标和调度生命周期。保留包名以减少无关重命名。service 通过 BetRepository 和 Provider 端口执行 I/O，不持有 HTTP 对象。
- internal/repository：SQLite schema、事务、投注与完整开奖缓存、发布同步进度；所有 SQL 和文件路径创建集中于此。
- internal/gateway：官方开奖 HTTP、分页、响应体限制、校验和安全错误；不决定何时重试官方发布。
- internal/httpapi：http 路由匹配、参数/方法/大小校验、响应与静态资源；只依赖 domain 和 BetService 端口，不访问数据库字段。net/http 小服务不另建无行为的 routes/controller 包。
- lottery：Go embed 和原有独立浏览器页面，维持 /、/lottery、资源名称和 CLI 的共享规则。
- internal/architecture：包依赖/纯逻辑约束测试。tools/check 是开发命令，不进入运行服务。

## 依赖方向

HTTP → BetService；lottery 用例 → domain 与 BetRepository/Provider 端口；gateway → domain；repository → domain。Provider、SnapshotProvider 和跨层 DTO 位于 domain，HTTP 不反向依赖具体服务。cmd 是组合根，可以组装全部层。domain 不导入 service、repository、gateway 或 HTTP I/O。Go 编译器拒绝循环，架构测试拒绝未知 internal 层与越界导入。

## 数据、并发与兼容

继续保持所有 /api/bets CRUD、/api/health 的状态码、JSON、分页参数兼容与静态 GET/HEAD；不存在公开手动验奖接口。调度只由进程内部启动。验奖任务互斥，官方发布进度控制请求，未来投注不能驱动额外拉取；失败不丢已有结果，已完整期号不能被旧响应覆盖，事务失败不写入部分开奖/结果。默认单实例写一个数据库；不能未经设计用多副本共享文件。

关闭顺序是取消任务/上游请求、关闭 HTTP、等待调度退出、关闭 SQLite，防止库关闭后后台写入。浏览器端金额/大整数仍按现有约束处理。包重排没有修改 schema、路由、API DTO 或嵌入资源公开路径。
