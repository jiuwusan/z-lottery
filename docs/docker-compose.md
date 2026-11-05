# Docker Compose 部署

沿用 `z-media-workflow` 的部署约定：`jiuwusan` Docker Hub 镜像、外部 `wk` 网络、命名数据卷、`restart: always` 和 Docker 日志轮转。页面内嵌于 Go 二进制，API、SQLite 和内置验奖调度在同一进程运行。运行镜像基于 Debian slim，包含 HTTPS CA 证书，二进制内嵌时区数据。使用非 root 的 UID/GID 1000，与旧镜像的运行用户一致，兼容现有卷权限。容器端口为 `3000`，宿主机默认 **3100**，避免与媒体服务的 3000 端口冲突。

## 配置

部署机器只需 `docker-compose.yml`，无需 `.env` 或令牌；本地构建还需要完整源码和 `docker-compose.build.yml`。

`HOST=0.0.0.0`、`PORT=3000`、`DATABASE_PATH=/app/data/lottery.sqlite` 固定在容器配置中，不受本地开发 `.env` 中这三个字段影响。需要调整访问端口时修改 `ports` 左侧的 `3100`，保留容器端口 `3000`。配置验证可运行 `docker compose config --quiet`。

`CHECK_ENABLED` 默认 `true`，`CHECK_SCHEDULE` 默认 `*/20 22-23 * * *;0 0 * * *`，`CHECK_TIMEZONE` 默认 `Asia/Shanghai`。这三个值可通过 `.env` 或宿主机环境变量覆盖；Compose 会传入容器，修改后重建容器生效。`CHECK_SCHEDULE` 使用分号分隔的五字段 cron 表达式。

先检查 `wk` 网络：

```bash
docker network ls --filter name=wk
```

仅在尚不存在时创建一次，已有 z-media-workflow 的 `wk` 网络可直接复用：

```bash
docker network create wk
```

## 从源码构建并启动

首次镜像尚未发布，或需要立即使用当前源码时，在项目根目录执行：

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
docker compose ps
docker compose logs -f --tail=100 z-lottery
```

访问 `http://服务器地址:3100/` 即可使用，投注记录自动加载，无需填写令牌。健康检查访问 `/api/health`，容器启动后可通过 `docker compose ps` 查看 `healthy` 状态。两个 Compose 文件合用时强制本地构建，不依赖 Docker Hub 上已经存在本项目镜像。

## 使用已发布镜像

需要先由 GitHub Actions 成功发布 `jiuwusan/z-lottery:latest`。本次代码修改只准备发布工作流，不会自行推送镜像。

```bash
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
```

更新服务执行 `docker compose pull` 和 `docker compose up -d`；固定版本时将 `image` 改为对应的 `sha-<短提交哈希>` 标签。命名卷 `lottery-data` 挂载到 `/app/data`，重建容器保留 SQLite 数据；不要使用 `docker compose down -v`，该操作会删除数据卷。[Compose 服务配置说明](https://docs.docker.com/reference/compose-file/services/)

日志为 `json-file`，单文件最多 10 MB，保留 3 个；容器停止等待为 15 秒，大于应用的 10 秒关闭超时。Dockerfile 通过多阶段 Go 构建（`CGO_ENABLED=0`，纯 Go SQLite），使用 exec 形式直接启动 `/app/z-lottery`，服务处理 SIGTERM/SIGINT，当前服务不创建业务子进程，Compose 不额外启用 init。镜像构建不包含 `.env`、数据库、测试、文档和 Git 元数据。

## 内置定时验奖

启用检查时启动立即补查，随后默认按北京时间每天 22:00、22:20、22:40、23:00、23:20、23:40 及次日 00:00 执行。午夜只检查 00:00，不会执行 00:20。无需安装 cron、HTTP 任务客户端或额外调度容器。

上游同步按彩种与窗口管理，即使没有投注也会同步官方开奖。00:00 属于前一天的 22:00–00:00 窗口。官方最新开奖须审核通过，且全部官方奖级金额完整，才停止该彩种在本窗口的后续请求。最新一期日期可以早于窗口日期，非开奖日无需等待不存在的当日记录。双色球要求一至六等奖，大乐透现行一至七等奖、历史规则一至九等奖，以及一、二等奖追加奖金。官方列表没有最新记录、最新记录未审核、缺少奖级金额或请求失败时，窗口内继续重试，不根据投注是否中奖或金额为 0 判定完成。

完成状态与开奖缓存保存在数据卷中的 SQLite（`draw_sync` 表按彩种与窗口记录状态）。同窗口重启补查复用完成状态，不重复访问上游；下一窗口重新发现官方开奖，不预推期号或硬编码开奖日历。本地投注匹配消费缓存，未来未公布的连续期数不会延长已完成窗口的网络轮询。新建回溯投注或旧记录缺少历史覆盖时，可进行一次必要回填，覆盖范围持久保存并复用。

检查失败保留已有数据，后续时刻重试；日志可用 `docker compose logs` 查看。设置 `CHECK_ENABLED=false` 会同时禁用启动补查和后续计划。

旧 `/api/jobs/check` 接口返回 404。升级时删除或停用已有宿主机 cron、容器定时任务和调度平台中的外部调用；页面不再提供复制验奖链接。

```bash
docker compose exec -T z-lottery /app/z-lottery healthcheck
```

## 反向代理

同处 `wk` 网络的 Nginx 可以代理整个站点，不必再单独复制静态文件：

```nginx
location / {
    proxy_pass http://z-lottery:3000;
    proxy_set_header Host $host;
    proxy_read_timeout 600s;
    client_max_body_size 512k;
}
```

宿主机运行的 Nginx 改用 `http://127.0.0.1:3100`。为外部访问配置自己的 HTTPS 域名和证书。若只需反向代理访问，可将端口映射改为 `127.0.0.1:3100:3000`，或在容器 Nginx 部署中移除 `ports`，继续使用 `wk` 网络访问。

## 从旧版服务升级与备份

Go 服务兼容已有 SQLite 投注表和 API 数据格式，自动创建官方同步状态等所需表，无需手动数据转换。已有命名卷继续使用，镜像保持 UID/GID 1000 与 `/app/data` 路径；新数据目录权限为 0700。升级前先停用外部 cron 和调度平台任务，停止旧服务并备份完整数据卷，然后更新镜像重建容器；检查健康状态、投注列表和服务日志。保留升级前镜像及备份以便恢复，本次代码更新不会自行部署或发布镜像。

可在服务停止后备份数据卷，以下使用 Compose 容器名 `z-lottery` 将完整数据目录复制到当前目录下的 `lottery-data-backup`（目标应为新的空目录）：

```bash
docker compose stop z-lottery
docker cp z-lottery:/app/data ./lottery-data-backup
# 确认备份包含数据库及存在的 WAL/SHM，再更新服务
docker compose pull
docker compose up -d
docker compose ps
```

从源码更新改用前述带 `docker-compose.build.yml` 的构建命令。备份包含投注信息，应保存到受控位置。

已有非容器数据库时，先停止原服务，再迁移已完整关闭的数据库至卷内。只在空卷尚无业务数据时执行迁移，避免覆盖容器内的新投注。以下命令中的旧库路径仅为示例：

```bash
docker compose stop z-lottery
docker cp ./data/lottery.sqlite z-lottery:/app/data/lottery.sqlite
docker compose run --rm --no-deps --user root z-lottery sh -c 'chown 1000:1000 /app/data/lottery.sqlite && chmod 600 /app/data/lottery.sqlite'
docker compose start z-lottery
```

若原服务未正常关闭，需先执行 SQLite 在线备份或同时处理 WAL 数据，不能只复制写入中的主库。迁移步骤适用于首次迁入空库；已有卷恢复前，应先备份整个卷并正确处理现有 WAL/SHM，不能直接覆盖运行中或已有 WAL 的数据库。正常维护可停止容器后备份数据卷，或使用 SQLite 在线备份工具。

## GitHub Actions 发布

`.github/workflows/docker-publish.yml` 参考 z-media-workflow 的发布方式：

1. 在 Docker Hub 创建 `jiuwusan/z-lottery` 仓库。
2. 在 GitHub 仓库配置 `OCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN` Secrets；用户名也兼容 `DOCKERHUB_USERNAME`，两者同时存在优先使用 `OCKERHUB_USERNAME`。账号必须有目标镜像推送权限。
3. 推送到 `master`，或手动运行 **Build and Push Docker Image**。

先执行 Go 测试、vet、race 与保留的 JS 测试，两种 Compose 配置验证，以及容器启动/二进制健康检查/UID 1000/旧任务接口 404/数据卷重建保留测试，再构建 `linux/amd64`、`linux/arm64`。`master` 发布 `latest`、`master` 和 `sha-<短提交哈希>`；手动构建其他分支不覆盖 `latest`。PR 仅测试和构建，不登录 Docker Hub 或推送。Docker Hub 发布凭据仅用于镜像推送，不传入应用镜像。
