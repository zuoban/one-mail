# One-Mail 项目概览

- 用途：现代化全栈邮件客户端，支持 IMAP 收发邮件。
- 技术栈：后端 Go 1.25+ / Gin / GORM / SQLite / go-imap；前端 React 19 / TypeScript / Vite / TailwindCSS 4。
- 认证：JWT，token 存 localStorage。
- 数据库：SQLite，默认 backend/data/one-mail.db，GORM AutoMigrate。

## 结构
- backend/cmd/server/main.go：入口
- backend/config/config.go, config.yaml：配置
- backend/database/database.go：GORM 连接
- backend/internal/handlers：HTTP 处理器
- backend/internal/services：业务逻辑（含 services/imap）
- backend/internal/models：GORM 模型
- frontend/src：React 前端（api/components/context/pages/config/types/index.css）
