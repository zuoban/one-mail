#!/bin/bash

# One-Mail 单容器启动脚本：秘密只从环境传入，不写入文件或命令行参数。
set -euo pipefail
export LC_ALL=C

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-one-mail}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
CONTAINER_NAME="one-mail"
PORT="${PORT:-80}"

if ! command -v docker >/dev/null 2>&1; then
    echo "错误：Docker 未安装。" >&2
    exit 1
fi

export ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
export CREDENTIALS_KEY="${CREDENTIALS_KEY:-}"

if ! docker image inspect "${IMAGE_NAME}:${IMAGE_TAG}" >/dev/null 2>&1; then
    echo "错误：镜像 ${IMAGE_NAME}:${IMAGE_TAG} 不存在，请先运行 ./docker-build.sh。" >&2
    exit 1
fi

if docker container inspect "${CONTAINER_NAME}" >/dev/null 2>&1; then
    echo "正在替换现有容器 ${CONTAINER_NAME}..."
    docker stop "${CONTAINER_NAME}" >/dev/null
    docker rm "${CONTAINER_NAME}" >/dev/null
fi

mkdir -p "${SCRIPT_DIR}/data"
docker run -d \
    -p "${PORT}:80" \
    -v "${SCRIPT_DIR}/data:/app/data" \
    --env ADMIN_PASSWORD \
    --env CREDENTIALS_KEY \
    --env "TZ=${TZ:-Asia/Shanghai}" \
    --name "${CONTAINER_NAME}" \
    --restart unless-stopped \
    "${IMAGE_NAME}:${IMAGE_TAG}"

echo "等待容器启动..."
sleep 5
if [[ "$(docker inspect --format '{{.State.Running}}' "${CONTAINER_NAME}")" != "true" ]]; then
    echo "错误：容器已退出，请查看 docker logs ${CONTAINER_NAME}。" >&2
    exit 1
fi

echo "容器已启动：http://localhost:${PORT}"
echo "健康检查：http://localhost:${PORT}/health"
echo "查看服务就绪状态：docker inspect --format '{{.State.Health.Status}}' ${CONTAINER_NAME}"
echo "查看日志：docker logs -f ${CONTAINER_NAME}"
echo "请持久化并备份 data 目录；未设置 CREDENTIALS_KEY 时，其中包含数据库凭证加密密钥。"
