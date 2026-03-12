.PHONY: run build frontend backend clean

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