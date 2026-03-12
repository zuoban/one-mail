# One-Mail 单镜像打包方案
# 前端构建阶段
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# 复制前端依赖
COPY frontend/package*.json ./
RUN npm ci

# 复制前端源码
COPY frontend/ ./

# 修改 API 基础 URL 为相对路径
RUN sed -i "s|baseURL: 'http://localhost:8080/api'|baseURL: '/api'|g" src/api/index.ts

# 构建前端
RUN npm run build

# 后端构建阶段
FROM golang:1.24-alpine AS backend-builder

WORKDIR /app/backend

# 安装依赖（包括 gcc 和 musl-dev 用于 CGO）
RUN apk add --no-cache git gcc musl-dev

# 启用自动工具链下载
ENV GOTOOLCHAIN=auto

# 复制 Go 依赖
COPY backend/go.mod backend/go.sum ./
RUN go mod download && \
    go install golang.org/dl/go1.25.8@latest && \
    go1.25.8 download

# 复制后端源码
COPY backend/ ./

# 更新 go.mod 并构建（启用 CGO）
RUN go1.25.8 mod tidy && \
    CGO_ENABLED=1 GOOS=linux go1.25.8 build -ldflags="-w -s" -o server ./cmd/server/main.go

# 最终运行阶段
FROM alpine:latest

WORKDIR /app

# 安装必要依赖（包括 libc6-compat 用于 CGO 二进制）
RUN apk add --no-cache ca-certificates nginx supervisor libc6-compat

# 创建必要的目录
RUN mkdir -p /app/data /run/nginx /var/log/supervisor

# 复制后端二进制和配置
COPY --from=backend-builder /app/backend/server /app/
COPY --from=backend-builder /app/backend/config.yaml /app/

# 复制前端构建产物
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html

# 复制 Nginx 配置
COPY docker/nginx-single.conf /etc/nginx/conf.d/default.conf
COPY docker/nginx.conf /etc/nginx/nginx.conf

# 复制 Supervisor 配置
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# 暴露端口
EXPOSE 80

# 使用 Supervisor 启动所有服务
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
