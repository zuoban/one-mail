#!/bin/bash

# One-Mail 单容器一键启动脚本

set -e

IMAGE_NAME="one-mail"
CONTAINER_NAME="one-mail"
PORT=80

echo "🚀 One-Mail 启动脚本"
echo "==================="
echo ""

# 检查镜像是否存在
if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "^${IMAGE_NAME}:${IMAGE_TAG}$"; then
    echo "❌ 镜像 ${IMAGE_NAME}:${IMAGE_TAG} 不存在"
    echo "请先运行: ./docker-build.sh"
    exit 1
fi

# 检查容器是否已存在
if docker ps -a | grep -q "${CONTAINER_NAME}$"; then
    echo "📝 容器已存在，正在重启..."
    docker stop ${CONTAINER_NAME} >/dev/null 2>&1 || true
    docker rm ${CONTAINER_NAME} >/dev/null 2>&1 || true
fi

# 创建数据目录
mkdir -p data

echo "🚀 启动容器..."
docker run -d \
    -p ${PORT}:80 \
    -v $(pwd)/data:/app/data \
    --name ${CONTAINER_NAME} \
    --restart unless-stopped \
    ${IMAGE_NAME}:latest

echo ""
echo "⏳ 等待服务启动..."
sleep 5

# 检查服务状态
if docker ps | grep -q "${CONTAINER_NAME}$"; then
    echo ""
    echo "✅ One-Mail 启动成功！"
    echo ""
    echo "🌐 访问地址:"
    echo "   - 前端界面: http://localhost"
    echo "   - API 接口: http://localhost/api"
    echo "   - 健康检查: http://localhost/health"
    echo ""
    echo "📊 常用命令:"
    echo "   - 查看日志: docker logs -f ${CONTAINER_NAME}"
    echo "   - 停止服务: docker stop ${CONTAINER_NAME}"
    echo "   - 删除容器: docker rm ${CONTAINER_NAME}"
    echo "   - 进入容器: docker exec -it ${CONTAINER_NAME} sh"
    echo ""
else
    echo ""
    echo "❌ 启动失败，查看日志:"
    echo "   docker logs ${CONTAINER_NAME}"
    exit 1
fi
