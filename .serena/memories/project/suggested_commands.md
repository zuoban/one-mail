# 常用命令

## 本地开发
- make run：同时启动前后端（需先 build）
- make backend：后端 :8080
- make frontend：前端 :5173

## 构建
- make build：构建前后端
- cd backend && go build -o server ./cmd/server/main.go：仅后端
- cd frontend && npm run build：仅前端

## 测试
- go test ./...：后端所有测试
- go test -v ./path -run TestName：运行单测

## Docker
- make docker-build
- make docker-run
- make docker-stop
- make docker-logs

## 前端
- cd frontend && npm run dev
- cd frontend && npm run lint
- cd frontend && npx tsc -b
