# 开发规范

适用范围：彩票选号与验奖服务 的运行源码、测试、浏览器与部署文件。职责和允许依赖见 [architecture.md](architecture.md)。按 Go 包职责组织，不机械套用 Node 目录，也不为层数创建无行为空包装。

## 环境与质量入口

Go 1.27.3；具体工具链以 go.mod 为准。modernc SQLite 提供无 CGO 持久化；robfig/cron 提供调度；godotenv 兼容现有 .env；goja 用于浏览器规则回归。

从仓库根目录执行：

    go mod download
    go run ./cmd/z-lottery
    go run ./cmd/lottery --help
    go run ./tools/check

check 依次检查 gofmt、go vet、全部 Go 测试（包含架构 import 门禁）和全部包构建，任一步失败即退出非零。Go 命令默认 -p 2，繁忙机器可设置 GOMAXPROCS=4；CI 继续运行现有 race/浏览器/容器门禁。修改后用 gofmt -w 指定 Go 文件，不整仓格式化无关代码。需要排查时执行 go test -p 2 -timeout 120s ./internal/具体包 -run TestName；交付仍须完整入口通过。

## 包、接口和代码风格

使用 gofmt、UTF-8、LF、末尾换行；.editorconfig/.gitattributes 固定编辑习惯。标准库、第三方和项目依赖分组；包名清晰且小写。优先小函数和有单一职责的类型，接口定义在调用方，传入 context、时钟和协议/存储端口使外部边界可测试。不新增万能 helper、跨包全局可变状态或无行为转发层。

运行源码不能从 init/包加载启动服务器、定时器或私有网络请求；组合根负责环境/生命周期。函数接收请求 context，耗时操作响应取消，关闭时取消并等待 goroutine、flush/关闭文件或数据库。对锁、事务和状态所有权给出说明，不能拿数据竞争被 race 发现之前当作安全证明。

internal/architecture 使用 Go parser 检查真实 import，注释和字符串不算依赖；新 internal 包必须先定义职责、允许矩阵和检查测试。Go 编译检查循环。测试可跨层集成；生产层不能因为测试便利导出内部数据库句柄、依赖 HTTP 对象或规避边界。

## 输入、错误与安全

HTTP 负责方法/路径、内容类型、请求体上限、JSON 基本结构和响应；用例负责资源状态、关联关系、数值/计划范围和持久化前业务校验；domain 负责纯规则。禁止依赖前端校验保证服务端安全。保留所有现有 URL、状态码、JSON 字段、Cookie/CSRF/Origin 顺序和 GET/HEAD 行为；未知错误只给客户端安全消息，日志不得透传凭据或上游响应正文。

Secret、Cookie、密码、Webhook、任务正文/头和生产数据不得进入日志、截图、测试 fixture 或 Git。使用虚构 URL 和本机 httptest；不读取生产 .env/配置/站点清单来验证重构，不连接真实 Mihomo、Docker 实例或通知机器人。新增网络请求须明确超时、响应大小、重定向、认证和错误脱敏。

## 持久化、时间与并发

DATABASE_PATH 指向 SQLite；保留 bets、bet_issues、draws、draw_sync、results 表与兼容旧 schema 的迁移。API 金额、期号、日期匹配和安全整数上限保持一致。

仓储拥有真实磁盘/SQL I/O，写失败不改变已提交状态；文件替换须保持原权限、临时文件清理和原子性。schema/JSON 变更另行设计迁移、备份和回滚，不能夹带在重构中；未知或无效版本不能静默覆盖。每个数据目录只允许既定单写者，不能直接加容器副本扩容。

HOST、PORT、DATABASE_PATH、CHECK_ENABLED、CHECK_SCHEDULE、CHECK_TIMEZONE，默认值和非法值处理以 cmd/z-lottery 的 loadConfig 为准。彩票业务时间使用 Asia/Shanghai；不能拿服务器本地日期替换。

时间规则使用现有时区，测试注入时钟或使用可控屏障；不把固定 sleep 当作竞态控制。HTTP 请求取消后不要写第二次响应；异步结果须核对 revision/实例生命周期，避免覆盖新配置或复活已删除资源。

## 浏览器、测试和依赖

浏览器与后端按 API 分界，不导入 Go 内部模块。保留桌面/移动布局、键盘与 label、对话框焦点、加载/禁用状态和错误展示；用户和上游文字使用 textContent/DOM，禁止未清洗 HTML 拼接。切换页面或实例后晚到响应不能覆盖当前状态。

行为修复先写失败回归测试再改实现；纯重构保留全部现有断言，按新边界迁移测试与 fixture。测试 domain 边界、service 用例/取消/失败、repository 临时文件/事务/重启、gateway 真实协议边界和 HTTP 鉴权/状态码/响应头；使用 t.TempDir/t.Cleanup，不发送真实通知或执行危险外部命令。

Go 测试包含 goja 执行浏览器彩票核心、CLI、HTTP CRUD、官方来源假响应、SQLite 迁移/事务/重启、调度与并发。开奖网关测试使用虚构传输，不请求真实开奖站。现有 CI 保留 race、Compose 和容器持久卷冒烟。

新增依赖必须说明用途、许可证和运行成本，使用 go get/go mod tidy 更新 go.mod/go.sum，不手改校验和。标准库足够时避免新增运行库。检查依赖改动与 Docker 构建环境一致，测试工具不能混进生产镜像。

## CI、部署与回滚

master push、面向 master 的 pull_request 和手动 workflow 运行质量门禁；镜像发布必须依赖 test 成功，PR 不登录或发布。分支保护的必需检查由维护者设置，工作流文件本身不能配置远端保护。

Dockerfile 编译 cmd/z-lottery；离线 CLI 通过 go build ./cmd/lottery 独立构建，保持原有无 CGO 构建、非 root 运行、健康检查及 /app/data 卷。发布前停写备份完整 SQLite（含 WAL/SHM 所需一致状态），优先使用 SQLite 一致备份或停机完整复制；不要运行中只复制主数据库文件。

发布前记录旧镜像 digest、运行配置和停写备份；备份放在独立受限位置并验证可恢复。升级后检查本地 health、只读 API、静态资源和挂载权限，不能用真实写操作/通知当作无意冒烟。回滚停止新实例，恢复旧 digest/配置和兼容的数据备份，不能删除数据卷或让新旧进程同时写入。

## Git 与交付

默认当前分支工作；未明确授权不得新建分支/worktree、提交、推送或部署。获准提交后使用 <类型>: <中文摘要>。当前项目位于 E:\personal\code，不需要飞书 ID；若迁移至 E:\code，须使用 <类型>: C<准确飞书ID>-<中文摘要>（端标识按实际 B/C/D 选择，默认 C），未知 ID 先获取不得编造。

交付说明必须列出实际修改、完整验证命令与结果、未执行的浏览器/容器/真实环境边界和实际风险；检查无密钥、临时程序、未关闭资源、无关格式化和运行数据。未执行不能报告成功；现有 CI 保留的附加检查不能用本地部分通过代替。
