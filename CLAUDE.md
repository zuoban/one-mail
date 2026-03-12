# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

One-Mail 是一个现代化的全栈邮件客户端应用，支持 IMAP 协议收发邮件。

### 技术栈
- **Backend**: Go 1.25+ / Gin / GORM / SQLite / go-imap
- **Frontend**: React 19 / TypeScript / Vite / TailwindCSS 4

## Common Commands

```bash
# 本地开发
make run              # 同时启动前后端 (需要先 build)
make backend          # 后端 :8080
make frontend         # 前端 :5173

# 构建
make build            # 构建前后端
cd backend && go build -o server ./cmd/server/main.go   # 仅后端
cd frontend && npm run build                             # 仅前端

# 测试
go test ./...                      # 后端所有测试
go test -v ./path -run TestName    # 运行单个测试

# Docker
make docker-build    # 构建镜像
make docker-run      # 运行容器 (端口 80)
make docker-stop     # 停止容器
make docker-logs     # 查看日志

# 前端开发
cd frontend && npm run dev          # 开发服务器
cd frontend && npm run lint          # ESLint 检查
cd frontend && npx tsc -b            # TypeScript 检查
```

## Architecture

### Backend (Go)
```
backend/
├── cmd/server/main.go      # 入口点
├── config/config.go        # 配置加载
├── config.yaml             # 配置文件
├── database/database.go    # GORM 连接
└── internal/
    ├── handlers/           # HTTP 处理器 (auth, account, email)
    ├── middleware/         # JWT 认证、CORS 中间件
    ├── models/models.go    # GORM 模型 (User, EmailAccount, Email)
    ├── services/imap/      # IMAP 客户端封装
    └── utils/              # JWT 工具
```

**API 分层**:
- `handlers/` - HTTP 请求处理，返回 JSON 响应
- `services/` - 业务逻辑，IMAP 邮件协议操作
- `models/` - GORM 数据模型

### Frontend (React)
```
frontend/src/
├── api/                    # Axios API 客户端
├── components/             # 通用组件
├── context/                # React Context (AuthContext)
├── pages/                  # 页面组件
├── config/                 # 前端配置
├── types/                  # TypeScript 类型定义
└── index.css               # TailwindCSS 全局样式
```

### Database
- SQLite，默认存储在 `backend/data/one-mail.db`
- GORM 自动迁移 (AutoMigrate)
- 主要模型: User, EmailAccount, Email

### Docker
- 单镜像方案，Nginx 反向代理
- 容器内: Nginx (80) → Go Backend (8080)
- 前端构建产物由 Nginx 直接服务
- 数据挂载 `./data` 目录持久化

## Key Files

- `backend/config.yaml` - 后端配置
- `backend/internal/handlers/` - API 路由实现
- `backend/internal/services/imap/client.go` - IMAP 协议封装
- `frontend/src/api/index.ts` - Axios 实例配置
- `frontend/src/context/AuthContext.tsx` - 认证状态管理

## Notes

- 前端使用 TailwindCSS 4，样式在 `index.css` 中配置
- API 认证使用 JWT，token 存放在 localStorage
- 密码字段在 JSON 序列化时使用 `json:"-"` 排除
- IMAP 同步为手动触发，使用 `/api/accounts/:id/sync`