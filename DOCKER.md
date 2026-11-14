# One-Mail Docker 部署指南

One-Mail 使用一个镜像运行 Nginx、Go 后端和前端静态资源，由 Supervisor 管理服务进程。

## 可选的访问密码与凭证密钥

One-Mail 供个人使用，没有注册、用户名、用户资料或应用内修改密码功能。设置非空的 `ADMIN_PASSWORD` 后，页面只需输入该密码；未设置或设为空字符串时，直接进入应用。修改环境变量后重启服务生效。会话最多有效 7 天，服务重启后需要重新登录。密码登录在实例范围内累计失败 10 次后暂时拒绝尝试，返回 HTTP 429 和 `Retry-After`，一分钟窗口结束后恢复；未开启密码时不受此限制。限流不信任客户端提供的代理 IP。

`CREDENTIALS_KEY` 用于加密邮箱凭证和 Telegram Bot Token，与访问密码独立。已有明文 Token 会在启动时加密迁移；密钥错误时拒绝启动，接口仅返回掩码。可以由部署平台注入 **32 字节随机数据的标准 Base64 编码**，也可以使用自动生成的密钥文件。需要显式设置密码和凭证密钥时，可在仓库根目录生成权限为 `600` 的 `.env`；以下命令需要 OpenSSL，不打印秘密并拒绝覆盖已有文件：

```bash
(
  set -eC
  umask 077
  {
    printf 'ADMIN_PASSWORD='
    openssl rand -hex 32
    printf 'CREDENTIALS_KEY='
    openssl rand -base64 32
  } > .env
)
```

当前 `.gitignore` 已忽略 `.env`、`.env.local` 和 `.env.*.local`。不要将秘密写入源码、Dockerfile、Compose 配置或提交到仓库，也不要在公开日志中打印环境文件或容器完整配置。已有环境文件应设置 `chmod 600 .env`；生产环境也可以由部署平台注入相同环境变量。

`CREDENTIALS_KEY` 可以不设置或留空。此时后端会在数据库同目录自动生成并读取权限为 `0600` 的密钥文件；默认容器路径为 `/app/data/one-mail.db.key`。**必须持久化并备份该文件和数据库**，否则无法解密已保存的邮箱凭证。显式注入的 `CREDENTIALS_KEY` 也必须单独安全备份，恢复时使用原密钥；不要直接替换密钥来尝试轮换。

## 使用 Docker Compose

Compose 会自动读取根目录 `.env`。不需要密码时，可直接启动，无需创建 `.env`；`ADMIN_PASSWORD` 默认为空。

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f
```

默认拉取 `ghcr.io/zuoban/one-mail:latest`，映射宿主机 `80` 端口，使用 `one-mail-data` 命名卷持久化 `/app/data`。`ADMIN_PASSWORD` 和 `CREDENTIALS_KEY` 均为可选环境变量，时区默认为 `Asia/Shanghai`。更新镜像后再次运行 `docker compose up -d` 即可重建容器并继续使用原数据卷。

## 本地构建和启动

构建使用 Go `1.25.8`、Node.js `22` 和 BuildKit 缓存，需要支持 BuildKit 的 Docker：

```bash
./docker-build.sh

# 可选：如创建了 .env，先加载；不配置密码可跳过。
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi
./docker-run.sh

# 也可以使用 Make
make docker-build
make docker-run
```

启动脚本通过 `docker run --env ADMIN_PASSWORD --env CREDENTIALS_KEY` 传入可选环境变量，不拼接真实值到命令行，也不会由脚本写入仓库文件。脚本将仓库根目录的 `data` 挂载到 `/app/data`。

`IMAGE_NAME`、`IMAGE_TAG` 可覆盖构建和启动镜像，`PORT` 可覆盖脚本映射的宿主机端口。例如：

```bash
IMAGE_NAME=one-mail IMAGE_TAG=local ./docker-build.sh
IMAGE_NAME=one-mail IMAGE_TAG=local PORT=8088 ./docker-run.sh
```

也可以直接使用 Docker；可选环境变量需先导出，然后执行：

```bash
docker build -t one-mail:latest .
mkdir -p data
docker run -d \
  -p 80:80 \
  -v "$(pwd)/data:/app/data" \
  --env ADMIN_PASSWORD \
  --env CREDENTIALS_KEY \
  --name one-mail \
  --restart unless-stopped \
  one-mail:latest
```

## 访问和健康检查

- 前端界面：http://localhost
- API：http://localhost/api
- 健康检查：http://localhost/health

镜像每 30 秒通过 Nginx 请求后端 `/health`，超时为 5 秒，启动宽限期为 30 秒，连续 3 次失败标记为不健康。后端以 500ms 预算检查数据库连通性，不可用时返回 HTTP 503。自定义 `PORT` 时，访问地址使用对应端口。

```bash
# 仅查看健康状态，不输出包含秘密的完整容器配置
docker inspect --format '{{.State.Health.Status}}' one-mail

# 查看日志、停止或删除脚本启动的容器
make docker-logs
make docker-stop

# Compose 部署停止并保留数据卷
docker compose down
```

容器默认设置 `GIN_MODE=release`，可用同名环境变量覆盖。后端以普通用户 `app` 运行；入口脚本将 `/app/data` 的所有权迁移给该用户并保留原密钥文件与权限。宿主机绑定目录的所有权也会随之变化，迁移前应备份数据，目录必须允许容器入口调整权限。

Supervisor、Nginx 和后端日志写入标准输出或标准错误，使用 `docker logs` 或平台日志采集。日志包含接口及账户刷新耗时、连接池（含临时测试连接）等待次数和累计时间、数据库等待指标、待发通知数及通知投递结果，不记录请求体或邮件主题。release 模式仍保留参数隐藏的慢 SQL 日志。

HTTP 服务设置 1 MiB 请求体限制、5 秒请求头预算、15 秒读取和 60 秒写入/空闲预算；全部账户刷新受整体 35 秒限制，每账户最多 20 秒，前端等待 45 秒。未完成账户以错误项返回，已成功账户保留提交结果。

停止时后端最多等待 45 秒完成在途请求；Compose 和运行脚本设置 100 秒容器停止宽限，使 Supervisor 有时间依次停止进程。手动启动容器时添加 `--stop-timeout 100`。

后台通知 worker 使用 SQLite outbox，失败任务按 10 秒起、最长 1 小时退避重试，重启后继续发送。通知任务保存摘要快照，即使邮件退出缓存窗口仍可投递；禁用通知暂停待发任务，重新启用后继续。账户删除或邮箱身份变化清理旧任务。投递为至少一次：Telegram 接受后、本地确认前中断可能导致重复。

停服先等待 HTTP 在途请求结束，再取消并等待后台通知与指标 worker，最后关闭 IMAP 池和数据库。未确认的通知任务留在数据库中；连接池关闭会中断拨号并释放空闲、使用中及临时连接。

## 数据持久化与备份

脚本使用宿主机 `./data`；Compose 使用 `one-mail-data` 命名卷。两种方式是独立的数据位置，切换部署方式时需要迁移数据库和密钥。

停服务后备份整个数据目录或数据卷，并安全备份 `.env` 或部署平台保存的凭证加密密钥。恢复数据库时同时恢复原凭证加密密钥，保留自动生成密钥文件的 `0600` 权限。重建镜像、删除容器或 `make clean` 不应删除持久化数据；不要使用 `docker compose down -v` 删除需要保留的数据卷。

从旧账号版本升级时，自动迁移保留邮箱配置、加密凭证和最近 50 封收件箱邮件的有效正文缓存，移除用户表与所有权字段。邮件改为只读缓存：清理旧删除记录、已删除账户及非收件箱缓存，移除已读/星标及待同步状态，物理清理窗口外的邮件和正文。Telegram 配置合并为全局单例：优先选择最近更新的完整配置，没有完整配置时保留最新记录。升级后旧用户登录令牌不再有效。

数据库以 `schema_migrations` 保存升级版本，历史结构与数据迁移在事务内只执行一次；每次启动仍校验全部密文和 FTS 索引。恢复或迁移时保留完整数据库（含迁移记录及通知任务）与原密钥，不手工清空版本表。

## 构建与持续集成

- 前端使用 Node.js 22；npm 下载缓存与源码分层，依次执行 `npm ci` 和构建。
- 后端直接使用 `golang:1.25.8-alpine`，启用 CGO 和 `fts5`，缓存 Go 模块与编译产物，以只读模块模式构建，不在镜像构建中运行 `go mod tidy` 或下载额外 Go wrapper。
- 最终运行镜像固定为受支持的 `alpine:3.23`，由 Nginx 监听 80 端口并代理内部 8080 端口上的后端。
- GitHub Actions 先运行 `go test -tags fts5 ./...` 和前端 `npm ci`、`npm run lint`、`npm run test`、`npm run build`。镜像任务通过显式 `needs` 依赖两个检查任务；任一失败都不会构建或发布镜像。
- 检查通过后构建 `linux/amd64`、`linux/arm64` 镜像；PR 只构建，主分支或版本标签推送时发布到 GHCR。digest 来自实际镜像构建步骤的输出。

## 相关文件

- `Dockerfile`：多阶段构建、运行时和健康检查。
- `.github/workflows/docker.yml`：发布前检查、多架构镜像构建与发布。
- `docker-compose.yml`：可选管理员密码、可选凭证密钥和持久化卷。
- `docker/nginx-single.conf`、`docker/nginx.conf`：静态资源和后端反向代理。
- `docker/supervisord.conf`：Nginx 和后端进程管理。
- `docker-build.sh`、`docker-run.sh`、`Makefile`：本地构建与部署入口。
