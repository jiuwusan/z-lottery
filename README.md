# 彩票随机选号脚本

`lottery.js` 是一个无需安装第三方依赖的命令行随机选号脚本，支持超级大乐透和双色球，可配置复式数量、多组投注、投注倍数、连续投注期数，以及多组之间共享的号码数量。

脚本只负责随机选号和金额计算，不提供彩票购买、中奖预测或验奖功能。

## 运行环境

- Node.js 18 或更高版本
- Windows、macOS 或 Linux 终端

查看帮助：

```bash
node lottery.js --help
```

直接运行时默认生成 4 组超级大乐透 `6+3` 复式号码：

```bash
node lottery.js
```

## 默认配置

| 彩票 | 前区/红球 | 后区/蓝球 | 组数 | 倍数 | 连续期数 |
|---|---:|---:|---:|---:|---:|
| 超级大乐透 | 6 | 3 | 4 | 1 | 1 |
| 双色球 | 7 | 2 | 4 | 1 | 1 |

使用双色球时需要明确指定 `--game ssq`：

```bash
node lottery.js --game ssq
```

## 参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `--game <dlt\|ssq>` | 彩票类型：`dlt` 为超级大乐透，`ssq` 为双色球 | `dlt` |
| `--front <数量>` | 前区或红球的选择数量 | 大乐透为 6，双色球为 7 |
| `--back <数量>` | 后区或蓝球的选择数量 | 大乐透为 3，双色球为 2 |
| `--count <数量>` | 生成多少组号码 | 4 |
| `--multiple <倍数>` | 每注投注倍数 | 1 |
| `--periods <期数>` | 使用相同号码连续投注多少期 | 1 |
| `--same <前区[,后区]>` | 多组之间共享的号码数量 | 不共享 |
| `--append` | 使用超级大乐透追加投注 | 不追加 |
| `--help` | 显示命令行帮助 | — |

## 多组共享号码

`--same` 用于控制多组号码中至少有多少个号码保持相同。

让 4 组大乐透号码共享 3 个前区号码和 2 个后区号码：

```bash
node lottery.js --game dlt --count 4 --same 3,2
```

只共享 3 个前区号码，后区不强制相同：

```bash
node lottery.js --game dlt --count 4 --same 3
```

只共享后区号码时，前区数量写为 `0`：

```bash
node lottery.js --game dlt --count 4 --same 0,2
```

公共号码会先随机生成，再用于每一组。其余位置从排除公共号码后的号码池中分别随机补齐，因此：

- 每一组内部不会出现重复号码。
- 非公共部分彼此独立，但可能偶然选中相同号码。
- 实际交集可能多于 `--same` 指定的数量，但不会少于该数量。
- `--same` 对单组投注没有实际效果。

共享数量不能超过本组实际选择的数量。例如 `--front 6` 时，不能使用 `--same 7`。

## 使用示例

### 大乐透单式

生成 5 组大乐透 `5+2` 单式号码：

```bash
node lottery.js --game dlt --front 5 --back 2 --count 5
```

### 大乐透复式、倍数和追加

生成 1 组大乐透 `7+3` 复式号码，投注 5 倍并追加：

```bash
node lottery.js --game dlt --front 7 --back 3 --count 1 --multiple 5 --append
```

### 双色球复式

生成 2 组双色球 `8 红 + 3 蓝` 复式号码：

```bash
node lottery.js --game ssq --front 8 --back 3 --count 2
```

### 连续投注

生成 1 组双色球号码，投注 3 倍并连续使用 10 期：

```bash
node lottery.js --game ssq --count 1 --multiple 3 --periods 10
```

`--periods` 不会为每期重新选号；同一次输出的号码会连续使用指定期数。

### 组合使用共享号码

生成 4 组双色球号码，每组选择 `8 红 + 3 蓝`，共享 4 个红球和 1 个蓝球，投注 2 倍并连续使用 5 期：

```bash
node lottery.js --game ssq --front 8 --back 3 --count 4 --same 4,1 --multiple 2 --periods 5
```

双色球不支持 `--append`。

## 注数和金额

单组基础注数按组合数计算：

```text
单组注数 = C(前区选择数, 前区单式数) × C(后区选择数, 后区单式数)
总注数 = 单组注数 × 组数
```

其中大乐透单式为 `5+2`，双色球单式为 `6 红 + 1 蓝`。

普通投注每注 2 元：

```text
每期普通金额 = 总注数 × 2 元 × 投注倍数
实际投注金额 = 每期金额 × 连续期数
```

大乐透使用 `--append` 时每注增加 1 元，即每注共 3 元：

```text
每期追加金额 = 总注数 × 3 元 × 投注倍数
```

示例：默认双色球每组 `7 红 + 2 蓝`，单组为 `C(7,6) × C(2,1) = 14` 注。默认生成 4 组，共 56 注，1 倍普通投注每期为 112 元。

## 参数限制

| 参数 | 大乐透 | 双色球 |
|---|---:|---:|
| `--front` | 5～35 | 6～33 |
| `--back` | 2～12 | 1～16 |
| `--count` | 大于 0 的整数 | 大于 0 的整数 |
| `--multiple` | 大于 0 的整数 | 大于 0 的整数 |
| `--periods` | 大于 0 的整数 | 大于 0 的整数 |

`--same` 必须使用非负整数，格式为 `前区数量` 或 `前区数量,后区数量`。以下写法无效：

```text
--same 3,
--same ,2
--same -1,0
--same 1.5,0
```

参数错误时，脚本会在标准错误中给出原因，并以非零状态退出。

## 浏览器页面

`lottery/` 提供纯 HTML/CSS/JavaScript 选号页面，支持与命令行相同的投注参数，无需安装或构建。可以直接打开 `lottery/index.html`，或复制整个目录到 Nginx 静态站点。部署说明见 [lottery/README.md](lottery/README.md)。

## 投注记录与自动验奖服务

新增 Node.js 服务层与 SQLite 数据持久层。页面使用随机选号表单新增投注，保存投注时间和连续期数；验奖时根据实际销售截止时间匹配后续连续开奖记录，不提前预测期号。支持投注列表、删除及逐期验奖详情，外部定时任务可直接调用验奖 API，无需令牌。

后端需要 Node.js 22.13+。直接运行 `npm start`，访问 `http://127.0.0.1:3000/` 即可使用；自定义端口或数据库路径时可复制 `.env.example` 为 `.env` 修改。生产部署需同时运行 Node.js 并让 Nginx 转发 `/api/`，原有离线选号仍可独立使用。

接口、数据结构、部署、外部 cron 调用及验奖范围见 [服务说明](server/README.md)。

## Docker Compose 部署

参考 z-media-workflow，使用 `jiuwusan/z-lottery:latest`、外部 `wk` 网络和 `lottery-data` 命名卷，页面与 API 共用容器，默认访问端口 `3100`。无需 `.env` 或令牌，可以从源码构建启动：

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

需先确认 `wk` 网络存在。镜像发布、部署更新、外部定时验奖、Nginx 和数据库迁移见 [Docker Compose 部署说明](docs/docker-compose.md)。

## 运行测试

```bash
npm test
```
