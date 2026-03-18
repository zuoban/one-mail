# 风格与约定

- 后端 Go（Gin/GORM），前端 React + TypeScript。
- 前端使用 TailwindCSS 4，样式集中在 frontend/src/index.css。
- API 认证 JWT，token 存 localStorage。
- 密码字段 JSON 序列化使用 `json:"-"` 排除。
