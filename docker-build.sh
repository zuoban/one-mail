#!/bin/bash

# One-Mail 单镜像 Docker 构建脚本

set -e

echo "🚀 One-Mail Docker 单镜像构建"
echo "=============================="
echo ""

# 检查 Docker
if ! command -v docker &> /dev/null; then
    echo "❌ 错误: Docker 未安装"
    exit 1
fi

echo "✅ Docker 已安装"
echo ""

# 设置镜像名称
IMAGE_NAME="one-mail"
IMAGE_TAG="latest"

# 创建数据目录
mkdir -p data

echo "📦 开始构建镜像..."
echo "   镜像名称: ${IMAGE_NAME}:${IMAGE_TAG}"
echo ""

# 构建镜像
docker build -t ${IMAGE_NAME}:${IMAGE_TAG} .

echo ""
echo "✅ 镜像构建成功！"
echo ""
echo "🚀 启动命令:"
echo "   docker run -d -p 80:80 -v \$(pwd)/data:/app/data --name one-mail ${IMAGE_NAME}:${IMAGE_TAG}"
echo ""
echo "🌐 访问地址:"
echo "   - 前端界面: http://localhost"
echo "   - API 接口: http://localhost/api"
echo "   - 健康检查: http://localhost/health"
echo ""
echo "📊 常用命令:"
echo "   - 启动: ./docker-run.sh"
echo "   - 停止: docker stop one-mail"
echo "   - 删除: docker rm one-mail"
echo "   - 日志: docker logs -f one-mail"
