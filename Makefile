.PHONY: run build frontend backend test-dev clean docker-build docker-run docker-start docker-stop docker-logs

run:
	node scripts/dev.mjs

test-dev:
	node --test scripts/dev.test.mjs

backend:
	cd backend && CGO_ENABLED=1 go run -tags fts5 ./cmd/server/main.go

frontend:
	cd frontend && npm run dev

build:
	cd backend && CGO_ENABLED=1 go build -tags fts5 -o server ./cmd/server/main.go
	cd frontend && npm run build

clean:
	rm -rf backend/server
	rm -rf frontend/dist

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
