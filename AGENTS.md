# AGENTS.md - One-Mail Project Guidelines

## Project Overview

One-Mail is a full-stack email client application with:
- **Backend**: Go 1.27.4 with Gin, GORM, SQLite WAL and optional FTS5
- **Frontend**: React 19 + TypeScript + Vite + TailwindCSS 4

## Build Commands

### Backend (Go)

```bash
# Run backend server
cd backend && CGO_ENABLED=1 go run -tags fts5 ./cmd/server/main.go

# Build backend binary
cd backend && CGO_ENABLED=1 go build -tags fts5 -o server ./cmd/server/main.go

# Run all tests
go test -race -tags fts5 ./...

# Run tests with verbose output
go test -v -tags fts5 ./...

# Run single test by name
go test -v ./internal/handlers -run TestAuthHandler

# Run tests in specific package
go test -v ./internal/models

# Format code
go fmt ./...

# Lint (requires golangci-lint installation)
golangci-lint run
```

### Frontend (TypeScript/React)

```bash
# Install dependencies
cd frontend && npm ci

# Run development server
cd frontend && npm run dev

# Build for production
cd frontend && npm run build

# Preview production build
cd frontend && npm run preview

# Lint code
cd frontend && npm run lint

# Type check
cd frontend && npx tsc -b

# Frontend regression tests (Vitest + Testing Library, jsdom)
cd frontend && npm test
```

### Full Stack (Makefile)

```bash
# Run both services and stop their process groups together (macOS/Linux/WSL)
make run

# Test the development supervisor with isolated fixture processes
make test-dev

# Repeatable synthetic benchmarks; see docs/performance.md
make bench

# Build everything
make build

# Clean build artifacts
make clean

# Docker commands
make docker-build    # Build Docker image
make docker-run      # Run in Docker container
make docker-stop     # Stop Docker container
make docker-logs     # View Docker logs
```

## Code Style Guidelines

### Go Backend

**Imports**: Group imports - standard library first, then third-party, then project imports
```go
import (
    "fmt"
    "net/http"
    
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    
    "one-mail/backend/database"
    "one-mail/backend/internal/models"
)
```

**Naming Conventions**:
- PascalCase for types, functions, exported variables
- camelCase for local variables, parameters
- snake_case for database columns via gorm tags
- Use meaningful names, avoid abbreviations except: `db`, `id`, `req`, `resp`, `cfg`

**Types & Structs**:
- Use struct tags for JSON/gorm: `json:"field_name" gorm:"column:field_name"`
- Use `gorm:"index"` for indexes, `gorm:"unique"` for unique constraints
- Password fields should have `json:"-"` to exclude from JSON
- Use `gorm.DeletedAt` for soft deletes

**Error Handling**:
- Return errors early, handle at handler level
- Use `c.JSON(status, gin.H{"error": "message"})` for errors
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`

**Handler Pattern**:
```go
func (h *AuthHandler) HandlerName(c *gin.Context) {
    var req RequestType
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    // business logic
}
```

### TypeScript Frontend

**Imports**: Use relative imports for internal modules
```typescript
import { useState } from 'react'
import axios from 'axios'
import { authApi } from '../api'
import useAuth from '../context/useAuth'
```

**Naming Conventions**:
- PascalCase for components and types/interfaces
- camelCase for variables, functions, hooks
- Prefix custom hooks with `use` (e.g., `useAuth`)
- Interface names should be noun-like (e.g., `User`, `EmailAccount`)

**Types**:
- Use TypeScript interfaces for API responses and data models
- Use explicit return types for complex functions
- Avoid `any`, use `unknown` when type is uncertain

**Components**:
- Use functional components with hooks
- Extract reusable logic into custom hooks
- Keep components focused (single responsibility)

**API Layer**:
- Use axios with interceptors for auth tokens
- Handle 401 responses to redirect to login
- Use `.then(r => r.data)` pattern for API calls

## Project Structure

```
one-mail/
├── backend/
│   ├── cmd/server/main.go      # Entry point
│   ├── config/config.go        # Configuration
│   ├── config.yaml             # Config file
│   ├── database/database.go    # Database connection
│   ├── internal/
│   │   ├── handlers/           # HTTP handlers
│   │   ├── middleware/         # Auth, CORS middleware
│   │   ├── models/             # GORM models
│   │   ├── services/           # Business logic (IMAP)
│   │   ├── application/        # Account, mail, notification orchestration
│   │   ├── access/             # Optional instance password access
│   │   ├── credentials/        # AES-GCM credential storage
│   │   └── inflight/           # Shared work with independent cancellation
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── api/                # API client (axios)
│   │   ├── components/         # React components
│   │   ├── context/            # React context (auth)
│   │   ├── pages/              # Page components
│   │   └── index.css           # Global styles
│   ├── package.json
│   ├── vite.config.ts
│   └── eslint.config.js
├── docker/
├── Dockerfile
├── Makefile
└── AGENTS.md
```

## Testing

- Tests should be placed in `*_test.go` files in the same package
- Frontend tests live in `frontend/tests`, using Vitest and Testing Library in jsdom
- Run single Go test: `go test -v -tags fts5 ./path -run TestName`
- Use Go 1.27.4 and Node.js 24.22.3/npm 11.19.1 (`mise.toml`, `frontend/.node-version`)
- Use temporary databases and fake transports in tests; never use real mailbox or Telegram credentials
- Preserve read-only mail, single-instance operation, single-connection SQLite, identity/version validation and encrypted credential storage

## Database

- Uses SQLite by default (configured in `backend/config.yaml`)
- Database file stored in `backend/data/one-mail.db`
- GORM creates missing current tables and indexes; no legacy schema/data migration
- Models defined in `backend/internal/models/models.go`

## Configuration

- Backend config: `backend/config.yaml`
- Server runs on port 8080 by default
- Frontend dev server: http://localhost:5173
- Frontend API base: same-origin `/api`, proxied to `http://127.0.0.1:8080` in development
