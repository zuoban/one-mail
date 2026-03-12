# AGENTS.md - One-Mail Project Guidelines

## Project Overview

One-Mail is a full-stack email client application with:
- **Backend**: Go 1.25+ with Gin web framework, GORM ORM, SQLite database
- **Frontend**: React 19 + TypeScript + Vite + TailwindCSS 4

## Build Commands

### Backend (Go)

```bash
# Run backend server
cd backend && go run ./cmd/server/main.go

# Build backend binary
cd backend && go build -o server ./cmd/server/main.go

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

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
cd frontend && npm install

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
```

### Full Stack (Makefile)

```bash
# Run both backend and frontend (requires backend binary built)
make run

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
│   │   └── utils/              # JWT, password utilities
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
- No frontend test framework configured yet
- Run single Go test: `go test -v ./path -run TestName`

## Database

- Uses SQLite by default (configured in `backend/config.yaml`)
- Database file stored in `backend/data/one-mail.db`
- GORM for ORM with auto-migration
- Models defined in `backend/internal/models/models.go`

## Configuration

- Backend config: `backend/config.yaml`
- Server runs on port 8080 by default
- Frontend dev server: http://localhost:5173
- API base URL: http://localhost:8080/api
