# One-Mail Docker 部署指南

One-Mail 使用一个镜像运行 Nginx、Go 后端和前端静态资源，由 Supervisor 管理服务进程。

## 先配置部署密钥

`JWT_SECRET` 必填，至少 32 字节，必须使用随机值。推荐另行设置 `CREDENTIALS_KEY`：它必须是 **32 字节随机数据的标准 Base64 编码**，用于加密邮箱凭证，不能与 JWT 密钥共用。

在仓库根目录执行以下命令，将随机密钥直接写入权限为 `600` 的 `.env` 文件，不在终端输出真实秘密。命令需要 OpenSSL，且会拒绝覆盖已有 `.env`：

```bash
(
  set -eC
  umask 077
  {
    printf 'JWT_SECRET='
    openssl rand -hex 32
    printf 'CREDENTIALS_KEY='
    openssl rand -base64 32
  } > .env
)
```

当前 `.gitignore` 已忽略 `.env`、`.env.local` 和 `.env.*.local`。不要将秘密写入源码、Dockerfile、Compose 配置或提交到仓库，也不要在公开日志中打印环境文件或容器完整配置。已有环境文件应设置 `chmod 600 .env`；生产环境也可以由部署平台注入相同环境变量。

`CREDENTIALS_KEY` 可以不设置或留空。此时后端会在数据库同目录自动生成并读取权限为 `0600` 的密钥文件；默认容器路径为 `/app/data/one-mail.db.key`。**必须持久化并备份该文件和数据库**，否则无法解密已保存的邮箱凭证。显式注入的 `CREDENTIALS_KEY` 也必须单独安全备份，恢复时使用原密钥；不要直接替换密钥来尝试轮换。

## 使用 Docker Compose

Compose 会自动读取根目录 `.env`。缺少或空的 `JWT_SECRET` 会在启动前报错；少于 32 字节或无效的密钥会被后端拒绝。

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f
```

默认拉取 `ghcr.io/zuoban/one-mail:latest`，映射宿主机 `80` 端口，使用 `one-mail-data` 命名卷持久化 `/app/data`。`CREDENTIALS_KEY` 是可选环境变量，时区默认为 `Asia/Shanghai`。更新镜像后再次运行 `docker compose up -d` 即可重建容器并继续使用原数据卷。

## 本地构建和启动

构建使用 Go `1.25.8`、Node.js `22` 和 BuildKit 缓存，需要支持 BuildKit 的 Docker：

```bash
./docker-build.sh

# 脚本只从已导出的环境读取秘密，不自动执行环境文件。
set -a
. ./.env
set +a
./docker-run.sh

# 也可以使用 Make（同样需要先加载环境变量）
make docker-build
make docker-run
```

启动脚本会先校验 `JWT_SECRET` 至少为 32 字节，再替换已有的 `one-mail` 容器。秘密通过 `docker run --env JWT_SECRET --env CREDENTIALS_KEY` 传入，不拼接真实值到命令行，也不会由脚本写入仓库文件。脚本将仓库根目录的 `data` 挂载到 `/app/data`。

`IMAGE_NAME`、`IMAGE_TAG` 可覆盖构建和启动镜像，`PORT` 可覆盖脚本映射的宿主机端口。例如，在加载密钥后执行：

```bash
IMAGE_NAME=one-mail IMAGE_TAG=local ./docker-build.sh
IMAGE_NAME=one-mail IMAGE_TAG=local PORT=8088 ./docker-run.sh
```

也可以直接使用 Docker；先按前面的步骤加载环境变量，再执行：

```bash
docker build -t one-mail:latest .
mkdir -p data
docker run -d \
  -p 80:80 \
  -v "$(pwd)/data:/app/data" \
  --env JWT_SECRET \
  --env CREDENTIALS_KEY \
  --name one-mail \
  --restart unless-stopped \
  one-mail:latest
```

## 访问和健康检查

- 前端界面：http://localhost
- API：http://localhost/api
- 健康检查：http://localhost/health

镜像每 30 秒通过 Nginx 请求后端现有 `/health`，超时为 5 秒，启动宽限期为 30 秒，连续 3 次失败标记为不健康。自定义 `PORT` 时，访问地址使用对应端口。

```bash
# 仅查看健康状态，不输出包含秘密的完整容器配置
docker inspect --format '{{.State.Health.Status}}' one-mail

# 查看日志、停止或删除脚本启动的容器
make docker-logs
make docker-stop

# Compose 部署停止并保留数据卷
docker compose down
```

## 数据持久化与备份

脚本使用宿主机 `./data`；Compose 使用 `one-mail-data` 命名卷。两种方式是独立的数据位置，切换部署方式时需要迁移数据库和密钥。

停服务后备份整个数据目录或数据卷，并安全备份 `.env` 或部署平台保存的密钥。恢复数据库时同时恢复原凭证加密密钥，保留自动生成密钥文件的 `0600` 权限。重建镜像、删除容器或 `make clean` 都不应删除持久化数据；不要使用 `docker compose down -v` 删除需要保留的数据卷。JWT 密钥应保持稳定，更换它会使现有登录令牌失效。

## 构建与持续集成

- 前端使用 Node.js 22；npm 下载缓存与源码分层，依次执行 `npm ci` 和构建。
- 后端直接使用 `golang:1.25.8-alpine`，启用 CGO 和 `fts5`，缓存 Go 模块与编译产物，以只读模块模式构建，不在镜像构建中运行 `go mod tidy` 或下载额外 Go wrapper。
- 最终运行镜像固定为受支持的 `alpine:3.23`，由 Nginx 监听 80 端口并代理内部 8080 端口上的后端。
- GitHub Actions 先运行 `go test -tags fts5 ./...` 和前端 `npm ci`、`npm run lint`、`npm run test`、`npm run build`。镜像任务通过显式 `needs` 依赖两个检查任务；任一失败都不会构建或发布镜像。
- 检查通过后构建 `linux/amd64`、`linux/arm64` 镜像；PR 只构建，主分支或版本标签推送时发布到 GHCR。digest 来自实际镜像构建步骤的输出。

## 相关文件

- `Dockerfile`：多阶段构建、运行时和健康检查。
- `.github/workflows/docker.yml`：发布前检查、多架构镜像构建与发布。
- `docker-compose.yml`：必填 JWT 环境变量、可选凭证密钥和持久化卷。
- `docker/nginx-single.conf`、`docker/nginx.conf`：静态资源和后端反向代理。
- `docker/supervisord.conf`：Nginx 和后端进程管理。
- `docker-build.sh`、`docker-run.sh`、`Makefile`：本地构建与部署入口。
