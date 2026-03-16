# One-Mail

<p align="center">
  <strong>一个现代化的全栈邮件客户端应用</strong>
</p>

<p align="center">
  <a href="#-技术栈">技术栈</a> •
  <a href="#-快速开始">快速开始</a> •
  <a href="#-部署">部署</a> •
  <a href="#-配置">配置</a> •
  <a href="#-api-接口">API</a>
</p>

---

## 📦 技术栈

### 后端
- Go 1.25+
- Gin Web Framework
- GORM ORM
- SQLite 数据库
- IMAP 邮件协议支持

### 前端
- React 19 + TypeScript
- Vite
- TailwindCSS 4

## 📖 项目结构

```
one-mail/
├── backend/               # Go 后端
│   ├── cmd/server/       # 入口点
│   ├── config/           # 配置
│   ├── database/         # 数据库连接
│   ├── internal/
│   │   ├── handlers/    # HTTP 处理器
│   │   ├── middleware/   # 中间件
│   │   ├── models/       # 数据模型
│   │   ├── services/    # 业务逻辑 (IMAP)
│   │   └── utils/        # 工具函数
│   └── data/             # SQLite 数据目录
├── frontend/             # React 前端
│   ├── src/
│   │   ├── api/         # API 客户端
│   │   ├── components/  # React 组件
│   │   ├── context/     # React Context
│   │   ├── pages/       # 页面组件
│   │   └── config/      # 前端配置
│   └── dist/            # 构建输出
├── docker/               # Docker 配置
├── Makefile             # 构建脚本
└── AGENTS.md            # 开发指南
```

## ⚡ 快速开始

### 前置要求

- Go 1.25+
- Node.js 18+
- npm 或 yarn

### 本地开发

1. 安装依赖

```bash
# 安装前端依赖
cd frontend && npm install

# 后端依赖 (在 go.mod 中)
cd backend && go mod download
```

2. 启动开发服务器

```bash
# 同时启动前后端
make run

# 或分别启动
make backend   # 后端 :8080
make frontend  # 前端 :5173
```

3. 访问应用

- 前端: http://localhost:5173
- 后端 API: http://localhost:8080
- 健康检查: http://localhost:8080/health

## 🚀 部署

### Docker 快速部署（推荐）

使用预构建镜像一键部署：

```bash
# 使用 Docker 运行
docker run -d \
  --name one-mail \
  -p 80:80 \
  -v one-mail-data:/app/data \
  ghcr.io/zuoban/one-mail:latest

# 访问应用
open http://localhost
```

### Docker Compose 部署

创建 `docker-compose.yml` 文件：

```yaml
version: '3.8'

services:
  one-mail:
    image: ghcr.io/zuoban/one-mail:latest
    container_name: one-mail
    restart: unless-stopped
    ports:
      - "80:80"
    volumes:
      - one-mail-data:/app/data
    environment:
      - TZ=Asia/Shanghai

volumes:
  one-mail-data:
    driver: local
```

启动服务：

```bash
# 启动
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止
docker-compose down
```

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/zuoban/one-mail.git
cd one-mail

# 构建 Docker 镜像
make docker-build

# 运行容器
make docker-run

# 查看日志
make docker-logs

# 停止容器
make docker-stop
```

### 数据持久化

容器使用 SQLite 数据库，数据存储在 `/app/data` 目录。建议使用 Docker Volume 持久化数据：

```bash
# 使用命名卷
docker run -v one-mail-data:/app/data ghcr.io/zuoban/one-mail:latest

# 或使用主机目录
docker run -v /path/to/data:/app/data ghcr.io/zuoban/one-mail:latest
```

### 环境配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `TZ` | 时区设置 | `UTC` |

### 反向代理配置

如果使用 Nginx 或 Caddy 作为反向代理：

**Nginx 配置示例：**

```nginx
server {
    listen 80;
    server_name mail.example.com;

    client_max_body_size 25M;

    location / {
        proxy_pass http://localhost:80;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**Caddy 配置示例：**

```
mail.example.com {
    reverse_proxy localhost:80
}
```

## 📡 API 接口

### 认证
- `POST /api/auth/register` - 用户注册
- `POST /api/auth/login` - 用户登录
- `GET /api/auth/me` - 获取当前用户

### 邮箱账户
- `GET /api/accounts` - 获取账户列表
- `POST /api/accounts` - 添加邮箱账户
- `DELETE /api/accounts/:id` - 删除账户
- `POST /api/accounts/:id/test` - 测试账户连接
- `POST /api/accounts/:id/sync` - 同步邮件

### 邮件
- `GET /api/emails` - 获取邮件列表
- `GET /api/emails/:id` - 获取邮件详情
- `POST /api/emails/:id/read` - 标记已读
- `POST /api/emails/:id/unread` - 标记未读
- `DELETE /api/emails/:id` - 删除邮件

## ⚙️ 配置

### 后端配置文件

编辑 `backend/config.yaml`：

```yaml
server:
  port: 8080
  host: "0.0.0.0"

database:
  path: "./data/one-mail.db"

jwt:
  secret: "your-secret-key"
  expiry_hours: 24
```

## 🔧 开发命令

```bash
# 构建
make build

# 清理
make clean

# 运行测试 (后端)
go test ./...
```

## 📄 许可证

[MIT](LICENSE)

---

<p align="center">
  Made with ❤️ by <a href="https://github.com/zuoban">zuoban</a>
</p>