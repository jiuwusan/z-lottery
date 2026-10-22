# Docker Compose 部署

沿用 `z-media-workflow` 的部署约定：`jiuwusan` Docker Hub 镜像、外部 `wk` 网络、命名数据卷、`restart: always` 和 Docker 日志轮转。页面与 API 在同一 Node.js 22 容器中运行，单进程，使用非 root 的 `node` 用户。容器端口为 `3000`，宿主机默认 **3100**，避免与媒体服务的 3000 端口冲突。

## 配置

部署目录需要 `docker-compose.yml` 和 `.env`；本地构建还需要完整源码和 `docker-compose.build.yml`。

首次将 `.env.example` 复制为 `.env`，将 `API_TOKEN` 和 `JOB_TOKEN` 替换为两个不同的随机令牌，至少 16 字符。管理令牌用于页面，任务令牌用于外部验奖。已有 `.env` 直接沿用，不要重新生成令牌或覆盖文件。

宿主机无需安装 Node.js，可运行以下命令生成随机令牌，执行两次并分别填入 `.env`：

```bash
docker run --rm node:22-slim node -p "require('node:crypto').randomBytes(32).toString('hex')"
```

Compose 仅从环境或 `.env` 读取两个令牌；`HOST=0.0.0.0`、`PORT=3000`、`DATABASE_PATH=/app/data/lottery.sqlite` 固定在容器配置中，不受本地开发 `.env` 中这三个字段影响。需要调整访问端口时修改 `ports` 左侧的 `3100`，保留容器端口 `3000`。不要使用会打印展开后令牌的 `docker compose config`，验证时加 `--quiet`。[Compose 环境变量说明](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)

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

访问 `http://服务器地址:3100/`，在“投注记录”中填写 `API_TOKEN`。健康检查访问 `/api/health`，容器启动后可通过 `docker compose ps` 查看 `healthy` 状态。两个 Compose 文件合用时强制本地构建，不依赖 Docker Hub 上已经存在本项目镜像。

## 使用已发布镜像

需要先由 GitHub Actions 成功发布 `jiuwusan/z-lottery:latest`。本次代码修改只准备发布工作流，不会自行推送镜像。

```bash
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
```

更新服务执行 `docker compose pull` 和 `docker compose up -d`；固定版本时将 `image` 改为对应的 `sha-<短提交哈希>` 标签。命名卷 `lottery-data` 挂载到 `/app/data`，重建容器保留 SQLite 数据；不要使用 `docker compose down -v`，该操作会删除数据卷。[Compose 服务配置说明](https://docs.docker.com/reference/compose-file/services/)

日志为 `json-file`，单文件最多 10 MB，保留 3 个；容器停止等待为 15 秒，大于应用的 10 秒关闭超时。镜像构建不包含 `.env`、数据库、测试、文档和 Git 元数据。

## 外部定时验奖

服务不自行安装或运行定时器。外部调度器调用：

```http
POST http://z-lottery:3000/api/jobs/check
Authorization: Bearer <JOB_TOKEN>
Content-Type: application/json

{}
```

同处 `wk` 网络的调度容器使用 `http://z-lottery:3000`；宿主机或远程调度器使用 `http://服务器地址:3100`，经反向代理则使用对应 HTTPS 域名。按响应的 `nextCursor` 继续调用，直到为 `null`。失败时重试，下次任务从空游标开始；完整协议见 [服务 API 说明](../server/README.md)。

宿主机可以直接让运行中的容器执行现有任务客户端，该客户端仍通过 HTTP 调用任务接口，并自动遍历分页；宿主机无需 Node.js 或另存任务令牌：

```bash
docker compose exec -T z-lottery node server/check-job.js
```

Linux cron 示例（替换工作目录和 Docker 路径，按宿主机时区运行）：

```cron
*/20 21-23 * * * cd /opt/z-lottery && /usr/bin/docker compose exec -T z-lottery node server/check-job.js >> /var/log/z-lottery-check.log 2>&1
```

## 反向代理

同处 `wk` 网络的 Nginx 可以代理整个站点，不必再单独复制静态文件：

```nginx
location / {
    proxy_pass http://z-lottery:3000;
    proxy_set_header Host $host;
    proxy_set_header Authorization $http_authorization;
    proxy_read_timeout 600s;
    client_max_body_size 512k;
}
```

宿主机运行的 Nginx 改用 `http://127.0.0.1:3100`。为外部访问配置自己的 HTTPS 域名和证书。若只需反向代理访问，可将端口映射改为 `127.0.0.1:3100:3000`，或在容器 Nginx 部署中移除 `ports`，继续使用 `wk` 网络访问。

## 已有数据迁移与备份

已有非容器数据库时，先停止原服务，再迁移已完整关闭的数据库至卷内。只在空卷尚无业务数据时执行迁移，避免覆盖容器内的新投注。以下命令中的旧库路径仅为示例：

```bash
docker compose stop z-lottery
docker cp ./data/lottery.sqlite z-lottery:/app/data/lottery.sqlite
docker compose run --rm --no-deps --user root z-lottery sh -c 'chown node:node /app/data/lottery.sqlite && chmod 600 /app/data/lottery.sqlite'
docker compose start z-lottery
```

若原服务未正常关闭，需先执行 SQLite 在线备份或同时处理 WAL 数据，不能只复制写入中的主库。迁移步骤适用于首次迁入空库；已有卷恢复前，应先备份整个卷并正确处理现有 WAL/SHM，不能直接覆盖运行中或已有 WAL 的数据库。正常维护可停止容器后备份数据卷，或使用 SQLite 在线备份工具。

## GitHub Actions 发布

`.github/workflows/docker-publish.yml` 参考 z-media-workflow 的发布方式：

1. 在 Docker Hub 创建 `jiuwusan/z-lottery` 仓库。
2. 在 GitHub 仓库配置 `OCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN` Secrets；用户名也兼容 `DOCKERHUB_USERNAME`，两者同时存在优先使用 `OCKERHUB_USERNAME`。账号必须有目标镜像推送权限。
3. 推送到 `master`，或手动运行 **Build and Push Docker Image**。

先执行全部测试、两种 Compose 配置验证及容器启动/健康检查/非 root/数据卷重建保留测试，再构建 `linux/amd64`、`linux/arm64`。`master` 发布 `latest`、`master` 和 `sha-<短提交哈希>`；手动构建其他分支不覆盖 `latest`。PR 仅测试和构建，不登录 Docker Hub 或推送。实际业务令牌不传入 Actions 或镜像构建。
