.PHONY: run build frontend backend clean docker-build docker-run docker-stop docker-logs

run: backend frontend

backend:
	cd backend && ./server

frontend:
	cd frontend && npm run dev

build:
	cd backend && go build -o server ./cmd/server/main.go
	cd frontend && npm run build

clean:
	rm -rf backend/server
	rm -rf frontend/dist
	rm -rf backend/data

# Docker 单镜像方案
docker-build:
	./docker-build.sh

docker-run:
	./docker-run.sh

docker-stop:
	docker stop one-mail || true
	docker rm one-mail || true

docker-logs:
	docker logs -f one-mail

# 便捷别名
docker-start: docker-run
