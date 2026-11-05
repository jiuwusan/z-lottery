# 投注服务与自动验奖

## 运行

推荐使用 [Docker Compose 部署](../docs/docker-compose.md)。服务使用 Go 1.27.3+、内嵌页面和时区数据，以及纯 Go 的 modernc SQLite 驱动；生产运行无需 CGO。

在项目根目录运行：

```bash
go run ./cmd/z-lottery
# 或编译后运行
go build -o z-lottery ./cmd/z-lottery
./z-lottery
```

访问 `http://127.0.0.1:3000/`。独立选号 CLI 使用 `go run ./cmd/lottery`，参数见根目录 README。

服务从工作目录自动加载 `.env`，已有环境变量优先；可复制 `.env.example` 修改。`HOST` 默认 `127.0.0.1`，`PORT` 默认 `3000`，`DATABASE_PATH` 默认 `./data/lottery.sqlite`，相对路径以进程工作目录为准。

当前为共享记录，所有页面和 API 均无需令牌，访问者管理同一批投注。SQLite 使用外键、WAL 和 busy timeout；每个数据库运行一个服务进程。删除投注级联清理其结果，重复验奖不会累加奖金。Go 服务兼容既有投注表和响应格式，自动增加官方同步状态等所需表，无需手动转换数据库。

## API

所有接口均无需令牌和认证请求头。带请求体的接口需要 `Content-Type: application/json`，上限 512 KiB；不开放跨域。

| 方法与路径 | 令牌 | 说明 |
|---|---|---|
| `GET /api/health` | 无 | `{ "ok": true }` |
| `GET /api/bets` | 无 | 分页列表 |
| `GET /api/bets/:id` | 无 | 投注详情，含各期开奖号码与逐组中奖结果 |
| `POST /api/bets` | 无 | 新增投注，成功返回 201 与已保存记录 |
| `DELETE /api/bets/:id` | 无 | 删除，成功返回 204，不存在返回 404 |

新增请求示例：

```json
{
  "game": "dlt",
  "tickets": [
    { "front": [1, 2, 3, 4, 5, 6], "back": [1, 2, 3] }
  ],
  "periods": 3,
  "placedAt": "2026-10-28T18:45:00+08:00",
  "multiple": 1,
  "append": true,
  "note": "连续三次实际开奖"
}
```

每条记录 1～1000 组，各组必须具有相同选号数量，组内不允许重复号码；各组可以相同。`periods` 为 1～100 的整数，默认 1。`placedAt` 可省略，此时采用服务端保存时间；指定时必须为带时区的有效 ISO 日期时间，且不能晚于服务器当前时间。统一以北京时间展示，数据库保存 UTC。金额由服务端按完整连续期数重算，不接受前端传入的金额。

### 按投注时间匹配实际开奖

页面“新增投注”复用随机选号表单，保存已展示的同一批随机号码、投注时间和期数。新记录为 `matchMode=date`，初始 `issues=[]`，不预测或锁定未来期号，保存时不访问外部开奖接口。修改设置后必须重新生成；同一次生成保存成功后按钮禁用，避免重复点击新增。

匹配步骤：

1. 上游同步独立于投注，先将官方开奖缓存到 SQLite，本地再按投注时间匹配。新投注或旧记录需要更早历史时，可以按需回填覆盖范围；每页 100 条，上限 50 页，成功覆盖的范围持久保存，后续轮次复用缓存。历史回填不是当前窗口持续轮询的停止条件。
2. 将完整记录按实际开奖日期升序排列，保留投注日期之前最近一期作为连续性校验锚点，检测缺页、漏期和未审核的中间记录。不能完整覆盖投注日期或存在缺口时，报错等待重试，不能拿较晚的开奖补位。
3. 比较投注时间与每期销售截止时间，严格满足 `placedAt < salesCloseAt` 才可参与。大乐透优先采用官方 `lotterySaleEndtime`，未提供时按当期规则补齐：23001 期起 21:00，此前 20:00；双色球按北京时间 20:00。不能直接比较只有年月日的开奖字段，也不能用直播开奖时刻代替销售截止时刻。
4. 取最早 N 次合格的真实开奖。已开奖不足 N 次时只保存已有结果，余下位置保持待匹配；后续任务继续补齐。休市、临时调整和跨年随实际历史记录，不维护预测休市日历，也不做期号加一。
5. 在同一事务中绑定实际期号并保存验奖结果。重试不得改变已经匹配的实际开奖前缀；上游返回与前缀冲突的数据时暂缓验奖，避免静默错配。缺失奖金保持待补全，不用后面的开奖替换。

例如，2026-10-28 18:45 的大乐透三期投注，会选择 10 月 28 日及之后最早的三次实际开奖；2026-10-28 21:00 或 21:15 投注，跳过当天，即使直播还未开始。时间比较按时区转换到同一时刻。

### 旧记录兼容

升级前记录保留为 `matchMode=issue`，按原来已保存期号验奖，不改写历史结果。API 显式传入 `issues` 也采用该兼容模式，期数取列表长度；新页面不传此字段。大乐透期号支持 `26120` 或 `2026120`，双色球使用 `2026120`。大乐透验奖支持 19019 期及以后。旧的预测期号接口与预测日历已移除。

列表参数：`page`（默认 1）、`pageSize`（默认 10，上限 50）、`game=dlt|ssq`、`status=pending|partial|prize_pending|won|lost`。返回 `{ items, total, page, pageSize }`。

记录含 `id/config/tickets/issues/matchMode/placedAt/note/totalAmount/amountPerPeriod/createdAt/status/results`，以及 `checkedPeriods/knownWinAmount/winAmount`。日期模式的 `issues` 为已匹配的真实期号列表，可能少于 `config.periods`；未匹配期数不是零期。每期结果包含 `draw/tickets/won/complete/knownAmount/amount/checkedAt`，`draw.salesCloseAt` 为实际使用的销售截止时刻；逐组 `winners` 包含奖级 `level`、含倍数的中奖注数 `count`、基础单注奖金 `unitAmount`、追加单注奖金 `appendUnitAmount` 和该奖级合计 `amount`。

状态：`pending` 未验出任何一期；`partial` 部分期已开奖；`prize_pending` 所有期均已出号码但有中奖奖金待补全；`won/lost` 所有期验奖及奖金已完整。`winAmount=null` 表示尚未全部完成，不是未中奖；`knownWinAmount` 为已经能够确认的部分奖金。单期缺失任一中奖奖级的奖金时，其 `amount=null`，完整奖级计入 `knownAmount`。

## 内置定时验奖

`CHECK_ENABLED` 默认 `true`。启用时启动立即补查一次，再按计划执行；设置 `false` 会禁用启动补查和后续计划。

`CHECK_SCHEDULE` 默认 `*/20 22-23 * * *;0 0 * * *`，采用五字段 cron，多条表达式用分号分隔；`CHECK_TIMEZONE` 默认 `Asia/Shanghai`。默认每天检查 22:00、22:20、22:40、23:00、23:20、23:40 和次日 00:00，包含午夜边界，不包含 00:20。检查时间采用配置的 IANA 时区，不依赖宿主机本地时区。

上游开奖同步与本地投注验奖分开执行。同步按彩种独立进行，即使没有投注也发现官方已发布的最新开奖；默认窗口为当日 22:00 至次日 00:00，00:00 属于前一天窗口。只有最新官方开奖已审核通过（`Ready`），且官方全部奖级金额完整（`OfficialAmountsComplete`）时，才停止该彩种在这个窗口的后续上游轮询。双色球要求一至六等奖；大乐透现行规则要求一至七等奖，历史九奖级规则要求一至九等奖，并要求一、二等奖追加奖金。不能仅根据本次投注命中奖级、未中奖 0 元或投注的 `winAmount` 判断上游同步完成。官方首页列表中其他已公布期次也会纳入完整性校验，不会因为最新一期完整而忽略其他期次的缺失奖金。

官方列表没有最新记录、最新记录未审核、缺少任一官方奖级或请求失败时，在窗口后续时刻继续同步。官方列表是唯一依据：最新记录日期可以早于窗口日期，非开奖日无需等待当日开奖。不预推今日或下一期期号，不硬编码开奖日历。同步状态按“彩种 + 窗口”写入 `draw_sync` 表，启动补查可复用同窗口已完成状态并跳过网络；进入下一窗口重新发现官方开奖。

本地验奖复用持久化的开奖缓存与历史覆盖范围，补齐已有实际开奖。连续投注未来尚未公布的期数保持待匹配，不会迫使已经同步完成的窗口继续发起 HTTP 请求。新建回溯投注或旧明确期号记录如缺少历史覆盖，可触发一次必要的历史回填；成功覆盖范围持久保存，后续检查与重启不重复抓取同一范围。历史回填与最新开奖的窗口完成状态分别管理。任务避免重叠，一个彩种或一条投注失败不阻止其他处理，详情可查看服务日志。

`/api/jobs/check` 已移除并返回 404；旧 HTTP 任务客户端和页面复制验奖链接均已删除。升级前停用宿主机 cron、容器定时任务和调度平台中已有的外部调用，服务内部直接执行检查。

## Nginx 与进程托管

Go 服务同时提供页面和 API，反向代理整个站点即可：

```nginx
location / {
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header Host $host;
    proxy_read_timeout 600s;
    client_max_body_size 512k;
}
```

配置自己的 TLS 证书。可用 systemd 托管编译好的二进制，以下示例假定已创建有权读写数据库的 `zlottery` 用户：

```ini
[Unit]
Description=z-lottery
After=network.target

[Service]
Type=simple
User=zlottery
WorkingDirectory=/opt/z-lottery
ExecStart=/opt/z-lottery/z-lottery
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

二进制读取工作目录下的 `.env`。可运行 `./z-lottery healthcheck` 验证 `HOST` / `PORT` 对应的健康接口。

环境文件仅允许运行账户读取。数据库备份建议停止服务后复制 SQLite 文件，或使用 SQLite `.backup`/在线备份功能；WAL 模式不能在写入中仅复制主文件。恢复时停止服务并恢复备份，再启动服务。

## 验奖依据与范围

- [大乐透现行规则](https://m.lottery.gov.cn/ksjz/m/yxgz_dlt/)、[19019 期起规则公告](https://sport.jining.gov.cn/art/2019/5/31/art_3807_1508029.html) 与 [官方计算器及 26014 期规则切换说明](https://m.lottery.gov.cn/mltsz/jsq/index.html)。19019～26013 期使用九奖级，26014 期起使用七奖级。
- [双色球游戏规则（湖南省民政厅发布）](https://mzt.hunan.gov.cn/mzt/fc/xwzxfc/tzggfc/202601/33894962/files/9b6723ae4d8743099b90d5edfe7d39ea.pdf)。
- 官方数据源使用福彩与体彩接口，适配实现见 `internal/lottery/provider.go`，日期模式完整分页读取历史开奖及奖金；兼容模式查询明确期号。大乐透销售截止规则见 [体彩官方时间调整公告](https://m.lottery.gov.cn/xxgk/tzggz/20221220/10027143.html)。

复式用命中与未命中号码的组合数统计各单式奖级，避免展开大量组合；每个单式只计最高奖级，但一组复式可以同时中多个奖级。倍数放大中奖注数；大乐透追加只参与一、二等奖，读取官方追加单注金额，不按浮点数乘 0.8 猜测取整。奖金使用分为单位检查安全精度。

展示普通规则下的**税前基础奖金及追加奖金**，不计算特别派奖、活动资格、兑奖税费。缺失奖金不会用固定金额猜测，按窗口继续同步官方完整奖级数据；已完成窗口复用缓存，下一窗口重新发现最新开奖。实际兑奖须以官方当期公告及实体彩票为准。

## 测试

```bash
go test ./...
go vet ./...
```

Linux CI 还运行 `go test -race ./...`，需启用 CGO 和 C 编译器。生产镜像仍以 `CGO_ENABLED=0` 构建。

包括 Go CLI 和由纯 Go goja 引擎执行的浏览器核心测试，以及服务端参数校验、复式奖级统计、规则切换、SQLite 重启/删除/分页、逐期幂等验奖、上游异常及 HTTP 请求限制、旧任务路径 404 和内置调度测试。测试使用隔离临时数据库与受控开奖数据，不依赖实时网络。
