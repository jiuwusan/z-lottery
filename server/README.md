# 投注服务与自动验奖

## 运行

推荐容器部署时参照 [Docker Compose 部署说明](../docs/docker-compose.md)，宿主机无需 Node.js，SQLite 使用持久卷。以下为直接运行 Node.js 的方式。

后端要求 **Node.js 22.13+**，使用内置 `node:http`、`node:sqlite` 和 `fetch`，无需安装第三方依赖。Node.js 22 的 SQLite 会输出 ExperimentalWarning，这是运行时提示。原有 CLI 和离线随机选号仍可使用 Node.js 18+。

1. 将根目录 `.env.example` 复制为 `.env`。
2. 生成随机令牌，填入 `JOB_TOKEN`（外部验奖任务）。例如运行 `node -p "require('node:crypto').randomBytes(32).toString('hex')"`。令牌至少 16 字符。
3. 在项目根目录运行 `npm start`，打开 `http://127.0.0.1:3000/`。投注记录自动加载，可直接新增和删除。无需 `npm install`。

配置：`HOST` 默认 `127.0.0.1`；`PORT` 默认 `3000`；`DATABASE_PATH` 默认项目内 `data/lottery.sqlite`。相对数据库路径以进程工作目录为准。`.env` 和数据库文件已忽略，不应提交。

当前为**共享记录**，页面及投注列表、新增、详情和删除接口无需认证，访问者管理同一批投注。仅外部验奖任务接口需要 `JOB_TOKEN`，该令牌不写入前端文件或 URL。

## 分层

- `app.js`：HTTP 路由、鉴权、请求限制、错误响应、静态文件白名单。
- `service.js`：投注用例、逐期验奖、批次游标、任务互斥。
- `domain.js`：投注验证、金额重算、复式组合计数及奖级规则。
- `repository.js`：SQLite 表、事务、分页、开奖缓存和验奖结果。
- `draw-provider.js`：官方历史列表分页、覆盖范围和连续性校验；兼容旧记录按期号查询。
- `draw-matching.js`：投注时间校验、北京时间和实际销售截止时间匹配。
- `index.js`：环境配置、启动和关闭。
- `check-job.js`：供外部定时任务使用的 HTTP 客户端，自动遍历全部批次。

SQLite 启用外键、WAL 和 5 秒 busy timeout。新投注先保存时间、号码和期数；验奖时在事务中绑定实际期号并保存结果。删除投注级联删除其期号与验奖结果。结果按“投注 ID + 期号”唯一保存，重复任务不会累加奖金。开奖号码单独缓存用于审阅，删除投注不清理公共开奖缓存。建议每个数据库运行一个服务进程，不使用多个副本共享网络文件系统数据库。旧数据库自动增加匹配模式和投注时间字段，保留旧记录及其期号/结果，不按不确定的历史登记时间重算。

## API

仅验奖任务接口使用 `Authorization: Bearer <JOB_TOKEN>`，投注接口无需令牌。带请求体的接口需要 `Content-Type: application/json`，上限 512 KiB；不开放跨域。

| 方法与路径 | 令牌 | 说明 |
|---|---|---|
| `GET /api/health` | 无 | `{ "ok": true }` |
| `GET /api/bets` | 无 | 分页列表 |
| `GET /api/bets/:id` | 无 | 投注详情，含各期开奖号码与逐组中奖结果 |
| `POST /api/bets` | 无 | 新增投注，成功返回 201 与已保存记录 |
| `DELETE /api/bets/:id` | 无 | 删除，成功返回 204，不存在返回 404 |
| `POST /api/jobs/check` | JOB_TOKEN | 查询官方开奖并验奖 |

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

1. 从官方历史接口分页读取，直到覆盖投注日期；每页 100 条，上限 50 页。同批同彩种共用历史请求。
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

## 外部定时任务

服务不会自行启动定时器。任务接口请求体可为 `{}`，也可指定：

```json
{ "game": "dlt", "limit": 20, "after": "dlt:26119" }
```

`game` 可省略；`limit` 默认 20，上限 100。日期模式按投注记录计数，兼容期号模式按不同“彩种+期号”计数。`after` 为上个响应的 `nextCursor`，首次省略，必须原样传回；日期模式游标为 `bet:<补齐16位的ID>`，兼容模式继续使用 `dlt:<期号>` 或 `ssq:<期号>`。

响应示例：

```json
{ "checked": 2, "waiting": 1, "updatedBets": 4, "errors": [], "nextCursor": null }
```

必须继续调用直到 `nextCursor=null`。每轮新任务从空游标开始，未匹配、奖金未补全和失败记录会再次检查，完成记录自动跳过。`checked` 为本批计算的期次数，重试可能包含先前已算过的期次，不代表新增结果；日期任务的 `updatedBets` 按更新的投注条数计，兼容任务按投注期记录数计。请求幂等；并发任务返回 409。任一上游失败返回 **502**，响应包含成功数量、错误和下一游标；日期任务错误带 `betId`，兼容任务错误带 `issue`。成功部分保留，失败投注不改变原状态。

提供的任务客户端会遍历分页、输出批次摘要，并在任何批次出错时以非零状态退出：

```bash
node --env-file=.env server/check-job.js
```

默认调用 `http://127.0.0.1:3000/api/jobs/check`（端口遵循 `PORT`），远程调度时配置 `JOB_URL=https://lottery.example.com/api/jobs/check` 与 `JOB_TOKEN`，该客户端本身不需要数据库文件。

Linux cron 示例，确保使用服务器实际的 node 路径与工作目录，按服务器时区每天 21～23 点每 20 分钟检查：

```cron
*/20 21-23 * * * cd /opt/z-lottery && /usr/bin/node --env-file=.env server/check-job.js >> /var/log/z-lottery-check.log 2>&1
```

也可以由现有调度平台直接发送 POST，令牌放请求头。建议调度平台在非 2xx 或网络失败时重试；同一任务客户端不应重叠运行。官方数据可能稍晚更新，因此建议多次执行，而不是只在开奖时间调用一次。

## Nginx 与进程托管

生产环境需要运行 Node.js 后端，不再只是复制 HTML。静态文件仍可复制 `lottery/` 到 `/var/www/z-lottery/`，服务代码放 `/opt/z-lottery/`，数据库放不可被 Nginx 静态访问的位置。

```nginx
server {
    listen 80;
    server_name lottery.example.com;
    root /var/www/z-lottery;
    index index.html;
    charset utf-8;

    location /api/ {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header Authorization $http_authorization;
        proxy_read_timeout 600s;
        client_max_body_size 512k;
    }

    location / {
        try_files $uri $uri/ =404;
    }
}
```

`proxy_pass` 末尾不加 `/`，保留 `/api/` 路径。若页面部署于 `/lottery/` 子路径，API 仍转发根路径 `/api/`。确保主配置加载 `mime.types`。配置自己的 TLS 证书后使用 HTTPS。

可用 systemd 运行后端，以下示例假定已创建有权读写数据库的 `zlottery` 系统用户：

```ini
[Unit]
Description=z-lottery API
After=network.target

[Service]
Type=simple
User=zlottery
WorkingDirectory=/opt/z-lottery
EnvironmentFile=/opt/z-lottery/.env
ExecStart=/usr/bin/node /opt/z-lottery/server/index.js
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

环境文件仅允许运行账户读取。数据库备份建议停止服务后复制 SQLite 文件，或使用 SQLite `.backup`/在线备份功能；WAL 模式不能在写入中仅复制主文件。恢复时停止服务并恢复备份，再启动服务。

## 验奖依据与范围

- [大乐透现行规则](https://m.lottery.gov.cn/ksjz/m/yxgz_dlt/)、[19019 期起规则公告](https://sport.jining.gov.cn/art/2019/5/31/art_3807_1508029.html) 与 [官方计算器及 26014 期规则切换说明](https://m.lottery.gov.cn/mltsz/jsq/index.html)。19019～26013 期使用九奖级，26014 期起使用七奖级。
- [双色球游戏规则（湖南省民政厅发布）](https://mzt.hunan.gov.cn/mzt/fc/xwzxfc/tzggfc/202601/33894962/files/9b6723ae4d8743099b90d5edfe7d39ea.pdf)。
- 官方数据源沿用项目原有两个抓取脚本指向的福彩与体彩接口，日期模式完整分页读取历史开奖及奖金；兼容模式查询明确期号。大乐透销售截止规则见 [体彩官方时间调整公告](https://m.lottery.gov.cn/xxgk/tzggz/20221220/10027143.html)。

复式用命中与未命中号码的组合数统计各单式奖级，避免展开大量组合；每个单式只计最高奖级，但一组复式可以同时中多个奖级。倍数放大中奖注数；大乐透追加只参与一、二等奖，读取官方追加单注金额，不按浮点数乘 0.8 猜测取整。奖金使用分为单位检查安全精度。

展示普通规则下的**税前基础奖金及追加奖金**，不计算特别派奖、活动资格、兑奖税费。缺失奖金不会用固定金额猜测，后续任务继续补全；完成后不会自动重新抓取已经验完的期号。实际兑奖须以官方当期公告及实体彩票为准。

## 测试

```bash
npm test
```

包括原 CLI/浏览器核心测试，以及服务端参数校验、复式奖级统计、规则切换、SQLite 重启/删除/分页、逐期幂等验奖、上游异常及 HTTP 鉴权/请求限制测试。测试使用隔离临时数据库与受控开奖数据，不依赖实时网络。
