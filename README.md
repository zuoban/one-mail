# One-Mail

<p align="center">
  <strong>供个人使用的最近邮件阅读器，支持接入多个邮箱</strong>
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
│   │   ├── application/ # 账户、邮件及通知应用服务
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
│   │   ├── accounts/    # 会话账户状态及邮箱表单
│   │   └── mail/        # 邮件状态、缓存及阅读组件
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

`make run` 在 macOS、Linux 或 WSL 中同时启动前后端，任一服务退出会停止另一个服务；`Ctrl+C`、SIGTERM 会回收两组进程及子进程，超过 5 秒未退出时强制回收。可用 `make test-dev` 运行隔离进程回归。使用 mise 时执行 `mise exec -- make run`，确保开发工具链与项目固定版本一致。

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
  --stop-timeout 100 \
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
    stop_grace_period: 100s
    ports:
      - "80:80"
    volumes:
      - one-mail-data:/app/data
    environment:
      - TZ=Asia/Shanghai
      - GIN_MODE=${GIN_MODE:-release}
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
| `CREDENTIALS_KEY` | 建议显式提供的 32 字节 Base64 密钥，用于 AES-GCM 加密邮箱凭证和 Telegram Bot Token | 未设置时在数据库旁生成权限 `600` 的 `.key` 文件 |
| `GIN_MODE` | 覆盖 Gin 运行模式 | Docker 为 `release`；本地使用配置文件 |

邮箱凭证和 Telegram Bot Token 从首次保存起以 AES-GCM 加密存储，启动时验证已保存的密文和密钥。必须备份数据库及其独立密钥；密钥遗失将无法恢复邮箱授权码和 Bot Token。

应用为单人模式，没有注册、用户资料或应用内修改密码功能。Telegram 使用一份全局配置。

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

密码登录在实例范围内累计失败 10 次后返回 HTTP 429 和 `Retry-After`，一分钟窗口结束后恢复。默认无密码模式不受此限流影响。

### 邮箱账户
- `GET /api/providers` - 获取服务商定义与自定义邮箱能力
- `GET /api/accounts` - 获取账户列表
- `POST /api/accounts` - 添加邮箱账户
- `PUT /api/accounts/:id` - 编辑账户，自定义邮箱支持 IMAP 主机及端口
- `DELETE /api/accounts/:id` - 删除账户
- `POST /api/accounts/:id/test` - 测试账户连接
- `POST /api/accounts/:id/refresh` - 拉取该账户最近的邮件，支持 `limit`，默认 50 封

邮箱账户页面提供添加、测试连接、编辑和删除操作；手动拉取邮件请使用收件箱中的「刷新」按钮。

添加账户时先输入邮箱地址，页面自动匹配 Gmail、QQ／Foxmail、iCloud、Outlook、163 的常见邮箱域名，无需选择服务商或重复填写登录用户名，新增账户直接使用邮箱地址登录。未识别的域名（含自有企业域名）显示 IMAP 主机和端口配置，使用 TLS 加密连接，默认端口为 `993`。修改邮箱地址时同步更新登录账号；邮箱地址未变时，保留原服务商、服务器配置和登录凭据。

输入 QQ 或 Foxmail 邮箱地址后，自动匹配「QQ 邮箱」，使用完整邮箱地址登录，IMAP 使用 `imap.qq.com:993`（TLS）。需先在 QQ 邮箱设置中开启 IMAP 服务，并使用授权码作为密码。客户端会等待服务器欢迎消息后再发送登录命令，避免 QQ 忽略提前发送的登录请求而超时。

输入 `@icloud.com`、`@me.com` 或 `@mac.com` 邮箱地址后，自动匹配「iCloud」，使用 `imap.mail.me.com:993`（TLS）收信。

密码须使用 **App 专用密码**：先开启 Apple 账户双重认证，再登录 [Apple 账户](https://account.apple.com/)，在「登录与安全」→「App 专用密码」中生成。修改或重置 Apple 账户主密码后，需重新生成并更新此密码。详见 [Apple 的服务器设置](https://support.apple.com/en-us/102525)及 [App 专用密码说明](https://support.apple.com/en-us/102654)。SMTP 预设为 `smtp.mail.me.com:587`；当前应用提供收信与阅读功能。

### 邮件
- `GET /api/emails` - 获取最近邮件，支持 `limit`、发件人或主题搜索及邮箱筛选，默认 50 封
- `GET /api/emails/:id` - 获取正文，本地未缓存时通过 IMAP 按需下载并落库
- `POST /api/emails/refresh` - 手动刷新全部或指定邮箱，支持 `limit`，默认每账户 50 封

邮件加载数量默认为 50，可在“设置 → 邮件阅读”选择 25、50、100 或 200 封，偏好保存在当前浏览器。列表及刷新接口接受 `limit`（1–200，省略或 0 时使用 50）；手动刷新按所选数量维护每个邮箱的缓存，汇总列表最多显示相同数量的最新邮件。调大后需手动刷新补拉更多邮件。刷新按账户身份版本、文件夹、UIDVALIDITY 与 UID 核对邮件身份，保留身份未变的正文缓存，物理清理窗口外的邮件及正文。更换服务商或用户名时原子清理旧缓存；轮换密码保留同一邮箱的缓存，并拒绝旧配置任务提交。新增摘要、旧缓存清理和刷新时间在同一事务中提交；无变化摘要不重复写入，获取或提交失败时保留原缓存，Telegram 新邮件通知在提交后发送。

收件箱会在当前浏览器记住所选邮箱（包括「所有邮箱」），重新加载页面或返回收件箱时恢复。账户列表读取中或读取失败时保留选择；成功读取后确认该邮箱已不存在时，自动回到「所有邮箱」。

使用收件箱「刷新」按钮或单键 `r` 拉取邮件；`Command + R`（macOS）和 `Ctrl + R` 保留浏览器的页面刷新行为，包括带 `Shift` 的刷新组合键。

搜索按输入的字面文本匹配主题、发件地址和发件人名称，`%`、`_`、反斜杠和双引号不作为查询语法。启用 FTS5 时至少 3 个字符使用 trigram 短语匹配，短查询及未启用 FTS5 的部署使用转义后的 LIKE。

服务商选项统一由后端提供。自定义邮箱新增时需传入 `imap_host` 与 `imap_port`（1–65535），编辑时可更新这两个字段；主机或端口变化按邮箱身份变更清理旧缓存及待发通知。账户已保存但首次同步失败时，新增接口仍返回账户及 `refresh_error`，可手动刷新，无需重复新增。

新增账户的连接验证、保存和首次同步共用 25 秒预算，前端等待上限为 35 秒；保存成功与首次同步失败分别反馈。同一 IMAP 主机、端口和登录用户名不可重复添加，重复请求返回 HTTP 409，提示刷新账户列表或编辑已有账户；并发新增及身份编辑通过实例内可取消的写入入口检查重复。删除账户后可重新添加。应用仍按单实例运行，不支持多个进程同时修改同一数据库。

Telegram 自动通知采用 SQLite outbox，邮件与任务同事务提交，发送失败退避重试、重启继续处理。首次导入和扩大窗口补拉的历史邮件不回放通知；已同步过的空邮箱收到第一封邮件会通知。禁用通知时暂停已有任务且不创建新任务，重新启用后继续；任务保留摘要快照，账户删除或身份变更时清理。投递为至少一次，远端已接受、本地尚未确认时崩溃可能造成重复。

新邮件通过用户主动刷新发现，当前没有定时收信或 IMAP IDLE；后台 worker 负责已经创建的通知投递及重试。

通知临时失败按 10 秒起、最长 1 小时退避，最多自动尝试 10 次；消息拒绝、Token 无效或聊天权限不足等永久错误直接进入失败状态。429 限流遵守 `retry_after`，整个通知队列的等待时间持久化，覆盖重启和等待期间新到的任务。更换 Bot 后仍遵守当前实例已记录的等待时间。完整消息（含标题、转义符和 Unicode）受 4096 UTF-16 单元预算限制。设置页显示待投递和失败数量，并提供失败任务重试、清理；已成功任务发送确认后删除，不保留成功历史。认证保护下的任务接口为 `GET /api/telegram/tasks?status=failed&limit=50`（最多 100 条）、`POST /api/telegram/tasks/:id/retry`、`DELETE /api/telegram/tasks/:id`，仅失败任务可修改，接口不返回邮件快照。

邮件为只读浏览：没有删除、已读/未读筛选或状态写回，打开正文不会修改远端已读状态。连接和刷新并发受全局及每账户限制。邮箱账户仍可删除，删除账户时清理其邮件缓存。

全部账户刷新最多执行 35 秒，每账户最多 20 秒，未完成账户返回明确错误。正文解析失败不会标记为已获取；摘要根据 MIME 结构显示附件标识，不下载附件正文。FTS 初始化检查索引完整性，失败时回退 LIKE。部署权限、日志、健康检查和优雅停止预算见 [Docker 部署文档](DOCKER.md)。

同一邮件正文和同一账户、身份版本、窗口数量的在途刷新共享远端请求；单个调用取消不影响其他等待者，全部等待者取消后中止共享工作。正文工作预算 10 秒，账户刷新工作预算 20 秒，不以首个调用者的剩余超时决定其他调用者的预算。不同窗口仍按账户串行执行。已有同一 UIDVALIDITY 的摘要时，先核对当前窗口 UID，只为缺少的 UID 获取 Envelope 和 BODYSTRUCTURE；首次导入或 UIDVALIDITY 变化仍完整获取。缓存窗口以本次 UID 核对结果为准，远端删除的邮件会清理。

数据库直接按当前模型创建表和索引，重启保留邮件缓存与通知任务。项目按全新部署维护，不提供历史版本数据库或明文凭证的升级迁移。

HTML 邮件在隔离文档中显示，清洗危险标签、属性及 URL。默认显示远程图片，可在“设置 → 邮件阅读”开启“阻止远程图片”，偏好保存在当前浏览器；开启阻止后仍可临时加载当前邮件的图片。其他远程资源继续阻止。

邮件 HTML 在内容或图片权限改变时重新清洗；只切换阅读主题时复用清洗结果，继续使用相同的 sandbox 和 CSP。Gzip 响应缓冲最多复用 256 KiB，超出容量的底层存储在请求结束后释放，避免池长期保留大正文。响应仍整块编码及压缩，这一上限不代表单请求峰值内存上限。

前端复用会话范围的账户状态与正文缓存。正文缓存按最近访问淘汰，最多 100 封、累计 16 MiB UTF-16 正文字符串；单封超过 2 MiB 仅保留当前阅读项。每次启动验证凭据密文及 FTS 一致性。

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
