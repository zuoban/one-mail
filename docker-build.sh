#!/bin/bash

# One-Mail 单镜像 Docker 构建脚本
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-one-mail}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

if ! command -v docker >/dev/null 2>&1; then
    echo "错误：Docker 未安装。" >&2
    exit 1
fi

echo "构建镜像 ${IMAGE_NAME}:${IMAGE_TAG}..."
docker build --tag "${IMAGE_NAME}:${IMAGE_TAG}" "${SCRIPT_DIR}"

echo "镜像构建成功。"
echo "请先按 DOCKER.md 配置并加载 JWT_SECRET（至少 32 字节），推荐独立设置 CREDENTIALS_KEY。"
echo "然后运行 ./docker-run.sh；自定义镜像时请传入相同的 IMAGE_NAME 和 IMAGE_TAG。"
echo "查看日志：docker logs -f one-mail"
