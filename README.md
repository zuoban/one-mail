# One-Mail

<p align="center">
  <strong>供个人使用的全栈邮件客户端，支持接入多个邮箱</strong>
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
│   │   └── access/       # 可选管理员密码鉴权
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
- Node.js 22+
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

默认不需要登录，启动后即可使用。需要访问密码时，在启动后端前设置 `ADMIN_PASSWORD`，页面只要求输入这个密码，无需注册或用户名：

```bash
export ADMIN_PASSWORD='your-private-password'
```

未设置或设为空字符串即关闭密码鉴权。密码变更后重启服务生效；重启后现有登录会话失效，需重新输入密码。邮箱凭证加密与数据备份见 [Docker 部署文档](DOCKER.md)。

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

前端默认通过同源 `/api` 请求后端。开发模式由 Vite 代理到 `http://127.0.0.1:8080`，Docker 部署由 Nginx 代理。若前后端部署在不同域名，可在前端构建时设置 `VITE_API_BASE_URL=https://api.example.com/api`，并在后端配置允许的跨域来源。

## 🚀 部署

### Docker 快速部署（推荐）

使用预构建镜像一键部署：

```bash
# 使用 Docker 运行
docker run -d \
  --name one-mail \
  --env ADMIN_PASSWORD \
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
      - ADMIN_PASSWORD=${ADMIN_PASSWORD:-}
      - CREDENTIALS_KEY=${CREDENTIALS_KEY:-}

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
docker run --env ADMIN_PASSWORD -v one-mail-data:/app/data ghcr.io/zuoban/one-mail:latest

# 或使用主机目录
docker run --env ADMIN_PASSWORD -v /path/to/data:/app/data ghcr.io/zuoban/one-mail:latest
```

### 环境配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `TZ` | 时区设置 | `UTC` |
| `ADMIN_PASSWORD` | 可选的管理员访问密码，非空时需要登录；修改后重启生效 | 空，直接使用 |
| `CREDENTIALS_KEY` | 建议显式提供的 32 字节 Base64 密钥，用于 AES-GCM 加密邮箱凭证 | 未设置时在数据库旁生成权限 `600` 的 `.key` 文件 |

邮箱凭证以 AES-GCM 加密保存，已有明文凭证会在首次升级时迁移。必须备份数据库及其独立密钥；旧备份可能仍含明文凭证，密钥遗失将无法恢复邮箱授权码。

应用为单人模式，没有注册、用户资料或应用内修改密码功能。升级会保留全部邮箱与邮件，并移除旧用户数据及所有权字段。Telegram 使用一份全局配置；旧库存在多份时保留最近更新的一份完整配置，无完整配置时保留最新记录。

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
- `GET /api/auth/status` - 查询是否需要管理员密码及当前会话是否有效
- `POST /api/auth/login` - 提交 `{ "password": "..." }` 验证管理员密码，成功返回 `{ "token": "..." }`

设置了管理员密码时，业务接口需要 `Authorization: Bearer <token>`；未设置时无需鉴权。会话最多有效 7 天，服务重启后失效。

### 邮箱账户
- `GET /api/accounts` - 获取账户列表
- `POST /api/accounts` - 添加邮箱账户
- `DELETE /api/accounts/:id` - 删除账户
- `POST /api/accounts/:id/test` - 测试账户连接
- `POST /api/accounts/:id/refresh` - 拉取该账户最近 50 封邮件

### 邮件
- `GET /api/emails` - 获取最近 50 封邮件列表
- `POST /api/emails/refresh` - 手动刷新邮件（拉取最近 50 封）
- `POST /api/emails/:id/read` - 标记已读
- `POST /api/emails/:id/unread` - 标记未读
- `DELETE /api/emails/:id` - 删除邮件

刷新按账户、文件夹、UIDVALIDITY 与 UID 核对本批邮件身份，旧邮件由数据库直接清理。新增邮件、已读/星标状态、旧邮件清理和刷新时间在同一事务中提交；任一步骤失败都会回滚并返回错误。Telegram 新邮件通知在事务提交后发送，本地删除的邮件不会因普通刷新恢复。

已读、未读和批量已读都会持久化待同步状态，并尝试写回 IMAP；远端不可用时响应包含 `sync_pending: true`，下次刷新继续同步，刷新不会覆盖待提交的用户操作。邮件通过 UIDVALIDITY 与 UID 校验远端身份；连接和刷新并发受全局及每账户限制。

HTML 邮件在隔离文档中显示，清洗危险标签、属性及 URL，默认阻止远程资源，用户可按邮件选择加载远程图片。

## ⚙️ 配置

### 后端配置文件

编辑 `backend/config.yaml`：

```yaml
server:
  mode: "release"
  port: 8080

database:
  type: "sqlite"
  path: "./data/one-mail.db"

```

## 🔧 开发命令

```bash
# 构建
make build

# 清理
make clean

# 运行测试 (后端)
cd backend && go test -tags fts5 ./...
```

前端回归、静态检查与构建：

```bash
cd frontend
npm test
npm run lint
npm run build
```

## 📄 许可证

[MIT](LICENSE)

---

<p align="center">
  Made with ❤️ by <a href="https://github.com/zuoban">zuoban</a>
</p>