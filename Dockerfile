# syntax=docker/dockerfile:1

# 前端依赖与源码分层缓存
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm run build

# 使用项目要求的 Go 工具链，保留 SQLite FTS5 与 CGO
FROM golang:1.25.8-alpine AS backend-builder
WORKDIR /app/backend
RUN apk add --no-cache gcc musl-dev
ENV GOTOOLCHAIN=local CGO_ENABLED=1
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -mod=readonly -trimpath -tags fts5 -ldflags="-w -s" -o server ./cmd/server/main.go

# 固定受支持的 Alpine 版本，运行 musl CGO 二进制
FROM alpine:3.23
WORKDIR /app
RUN apk add --no-cache ca-certificates nginx supervisor tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app \
    && mkdir -p /app/data /run/nginx \
    && chown app:app /app/data \
    && chmod 0700 /app/data
ENV GIN_MODE=release

COPY --from=backend-builder /app/backend/server /app/
COPY --from=backend-builder /app/backend/config.yaml /app/
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html
COPY docker/nginx-single.conf /etc/nginx/conf.d/default.conf
COPY docker/nginx.conf /etc/nginx/nginx.conf
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY --chmod=755 docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://127.0.0.1/health || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
