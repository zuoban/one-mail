# One-Mail

一个现代化的全栈邮件客户端应用。

## 技术栈

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

## 项目结构

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

## 快速开始

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

### Docker 部署

```bash
# 构建 Docker 镜像
make docker-build

# 运行容器
make docker-run

# 查看日志
make docker-logs

# 停止容器
make docker-stop
```

## API 接口

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

## 配置

### 后端配置

编辑 `backend/config.yaml`:

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

## 开发命令

```bash
# 构建
make build

# 清理
make clean

# 运行测试 (后端)
go test ./...
```

## 许可证

MIT