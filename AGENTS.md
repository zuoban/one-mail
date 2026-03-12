# AGENTS.md - One-Mail Project Guidelines

## Project Overview

One-Mail is a full-stack email client application with:
- **Backend**: Go 1.26+ with Gin web framework, GORM ORM, SQLite database
- **Frontend**: React 19 + TypeScript + Vite + TailwindCSS 4

## Build Commands

### Backend (Go)

```bash
# Run backend server
cd backend && go run ./cmd/server/main.go

# Build backend binary
cd backend && go build -o server ./cmd/server/main.go

# Run tests (no tests exist yet - create in same directory as code)
go test ./...

# Run single test
go test -v ./internal/handlers -run TestAuthHandler

# Lint (install golangci-lint first)
golangci-lint run

# Format code
go fmt ./...
```

### Frontend (TypeScript/React)

```bash
# Install dependencies
cd frontend && npm install

# Run development server
cd frontend && npm run dev

# Build for production
cd frontend && npm run build

# Lint code
cd frontend && npm run lint

# Run single test (if tests exist)
npm test -- --run

# Type check
cd frontend && npx tsc -b
```

### Full Stack

```bash
# Run both backend and frontend
make run

# Build everything
make build

# Clean build artifacts
make clean
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
- Use PascalCase for types, functions, exported variables
- Use camelCase for local variables, parameters
- Use snake_case for database columns (via gorm tags)
- Prefix interfaces with handler/service/repository names (e.g., `AuthHandler`)
- Use meaningful names, avoid abbreviations except: `db`, `id`, `req`, `resp`, `cfg`

**Types & Structs**:
- Use struct tags for JSON/gorm serialization: `json:"field_name" gorm:"column:field_name"`
- Use `gorm:"index"` for indexes, `gorm:"unique"` for unique constraints
- Password fields should have `json:"-"` to exclude from JSON responses
- Use `gorm.DeletedAt` for soft deletes

**Error Handling**:
- Return errors early, handle at handler level
- Use `c.JSON(status, gin.H{"error": "message"})` for error responses
- Log errors before returning when appropriate
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`

**Handler Pattern**:
```go
func (h *HandlerType) HandlerName(c *gin.Context) {
    var req RequestType
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    // business logic
}
```

### TypeScript Frontend

**ESLint Configuration**: Uses flat config with React hooks, TypeScript, and Vite plugins

**Imports**: Use absolute imports from `@/` for internal modules
```typescript
import { useState } from 'react'
import axios from 'axios'
import { authApi, accountApi } from '@/api'
```

**Naming Conventions**:
- Use PascalCase for components and types/interfaces
- Use camelCase for variables, functions, hooks
- Prefix custom hooks with `use` (e.g., `useAuth`, `useEmailList`)
- Interface names should be noun-like (e.g., `User`, `EmailAccount`)

**Types**:
- Use TypeScript interfaces for API responses and data models
- Use explicit return types for functions when complex
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
│   ├── database/database.go    # Database connection
│   ├── internal/
│   │   ├── handlers/           # HTTP handlers
│   │   ├── middleware/         # Auth, CORS middleware
│   │   ├── models/             # GORM models
│   │   ├── services/           # Business logic
│   │   └── utils/              # JWT, password utilities
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── api/                # API client
│   │   ├── components/         # React components
│   │   ├── pages/              # Page components
│   │   └── ...
│   ├── package.json
│   ├── vite.config.ts
│   └── eslint.config.js
├── Makefile
└── AGENTS.md
```

## Testing

- Tests should be placed in `*_test.go` files in the same package
- Frontend tests use Vitest (if added) - place in `*.test.ts` or `*.spec.ts`
- Run single test: `go test -v ./path -run TestName`
- No test framework configured yet - consider adding testify for Go

## Database

- Uses SQLite by default (configured in `config.yaml`)
- GORM for ORM with auto-migration
- Models defined in `backend/internal/models/models.go`

## Configuration

- Backend config: `backend/config.yaml`
- Server runs on port 8080 by default
- Frontend dev server: http://localhost:5173
- API base URL: http://localhost:8080/api