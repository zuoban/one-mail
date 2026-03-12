# One-Mail Docker 部署指南

## 单镜像方案

将前后端服务打包到一个 Docker 镜像中，使用 Supervisor 管理进程。

## 快速开始

```bash
# 构建镜像
./docker-build.sh

# 启动服务
./docker-run.sh

# 或者使用 Make
make docker-build
make docker-run
```

## 使用说明

### 构建镜像

```bash
./docker-build.sh
```

### 启动容器

```bash
./docker-run.sh
```

容器会自动：
- 暴露 80 端口
- 挂载 `./data` 目录持久化数据
- 自动重启（unless-stopped）

### 常用命令

```bash
# 停止容器
make docker-stop

# 查看日志
make docker-logs

# 直接使用 Docker
docker build -t one-mail .
docker run -d -p 80:80 -v $(pwd)/data:/app/data --name one-mail one-mail
```

## 访问地址

- 前端界面: http://localhost
- API 接口: http://localhost/api
- 健康检查: http://localhost/health

## 数据持久化

SQLite 数据库挂载在 `./data` 目录，重启容器数据不会丢失。

## 技术架构

- **基础镜像**: Alpine Linux
- **进程管理**: Supervisor (管理 Nginx + Go)
- **Web 服务器**: Nginx (监听 80 端口)
- **后端服务**: Go 服务 (监听 8080 端口，内部代理)
- **前端**: React 构建产物由 Nginx 直接服务
- **API**: Nginx 反向代理到后端

## 文件说明

- `Dockerfile` - 多阶段构建配置
- `docker/nginx-single.conf` - Nginx 站点配置
- `docker/nginx.conf` - Nginx 主配置
- `docker/supervisord.conf` - Supervisor 进程管理
- `docker-build.sh` - 镜像构建脚本
- `docker-run.sh` - 容器启动脚本
