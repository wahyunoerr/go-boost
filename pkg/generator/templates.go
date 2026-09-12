package generator

import (
	"fmt"
	"strings"

	"github.com/wahyunoerr/go-boost/pkg/detector"
)

func GenerateAgentsMD(stack *detector.ProjectStack) string {
	var sb strings.Builder

	sb.WriteString("# Go Project Engineering Guidelines (go-boost)\n\n")
	sb.WriteString(fmt.Sprintf("This project `%s` is powered by **Go %s** and **go-boost**.\n\n", stack.ModuleName, stack.GoVersion))

	sb.WriteString("## Detected Stack Architecture\n")
	sb.WriteString(fmt.Sprintf("- **Framework**: `%s` (v%s)\n", stack.Framework, stack.FrameworkVersion))
	sb.WriteString(fmt.Sprintf("- **ORM / Database Layer**: `%s` (v%s)\n", stack.ORM, stack.ORMVersion))
	sb.WriteString(fmt.Sprintf("- **Database Engine**: `%s`\n", stack.DatabaseEngine))
	if stack.CacheEngine != "" && stack.CacheEngine != "none" {
		sb.WriteString(fmt.Sprintf("- **Cache Engine**: `%s`\n", stack.CacheEngine))
	}
	if stack.QueueEngine != "" && stack.QueueEngine != "none" {
		sb.WriteString(fmt.Sprintf("- **Queue Engine**: `%s`\n", stack.QueueEngine))
	}
	if stack.RPC != "" && stack.RPC != "none" {
		sb.WriteString(fmt.Sprintf("- **RPC**: `%s`\n", stack.RPC))
	}
	if stack.ConfigManager != "" && stack.ConfigManager != "none" {
		sb.WriteString(fmt.Sprintf("- **Config Manager**: `%s`\n", stack.ConfigManager))
	}
	sb.WriteString(fmt.Sprintf("- **Logger**: `%s`\n", stack.Logger))
	sb.WriteString(fmt.Sprintf("- **Architecture**: `%s`\n\n", stack.Architecture))

	sb.WriteString("## Available MCP Tools via `go-boost`\n")
	sb.WriteString("You have access to the `go-boost` MCP server with 11 native tools:\n")
	sb.WriteString("1. `app_info`: Call upfront to see complete project metadata & packages.\n")
	sb.WriteString("2. `ast_inspect`: Deeply inspect Go structs, interfaces, and field tags (`json`, `gorm`, `validate`).\n")
	sb.WriteString("3. `route_list`: Statically view all registered HTTP endpoints and handlers.\n")
	sb.WriteString("4. `db_schema`: Inspect database tables, column types, keys, and foreign relations.\n")
	sb.WriteString("5. `db_query`: Execute safe, read-only SQL queries (`SELECT`, `SHOW`, `EXPLAIN`).\n")
	sb.WriteString("6. `db_connections`: Discover configured databases.\n")
	sb.WriteString("7. `read_logs`: Read and parse application logs (`slog`, `zap`, `zerolog`).\n")
	sb.WriteString("8. `last_error`: Get stack traces of recent backend errors or panics.\n")
	sb.WriteString("9. `test_runner`: Run targeted Go unit tests and view structured failure diagnostics.\n")
	sb.WriteString("10. `go_doc`: Check official Go stdlib or third-party symbol documentation.\n")
	sb.WriteString("11. `code_check`: Run `go vet` to ensure generated code compiles cleanly.\n\n")

	sb.WriteString("## Go Coding Standards & Best Practices\n")
	sb.WriteString("- **Context Propagation**: Always pass `ctx context.Context` as the very first parameter to I/O functions, handlers, and repositories.\n")
	sb.WriteString("- **Error Handling**:\n")
	sb.WriteString("  - Wrap errors with context using `fmt.Errorf(\"...: %w\", err)`.\n")
	sb.WriteString("  - Inspect errors using `errors.Is(err, target)` and `errors.As(err, &target)`.\n")
	sb.WriteString("  - Never ignore returned errors with `_` unless explicitly justified.\n")
	sb.WriteString("- **Modern Go Idioms** (Go 1.22+):\n")
	sb.WriteString("  - Standard loop variable scoping (each iteration creates a new variable; no closure leaks).\n")
	sb.WriteString("  - Standard `slices` and `maps` packages over manual loops where readable.\n")
	if stack.Logger == "slog" {
		sb.WriteString("  - Use structured logging via `log/slog`: `slog.InfoContext(ctx, \"...\", \"key\", val)`.\n")
	} else if stack.Logger == "zap" {
		sb.WriteString("  - Use Uber Zap structured logging: `logger.Info(\"...\", zap.String(\"key\", val))`.\n")
	}
	sb.WriteString("- **Testing**: Write idiomatic table-driven tests with `t.Run(tc.name, func(t *testing.T) { ... })`.\n")

	return sb.String()
}

func GenerateClaudeMD(stack *detector.ProjectStack) string {
	return GenerateAgentsMD(stack)
}

func GenerateCursorRules(stack *detector.ProjectStack) string {
	return fmt.Sprintf(`Cursor Rules: %s (Go %s)
You are an expert Go backend engineer working on this %s application.
Architecture: %s | Web Framework: %s | ORM: %s | Logger: %s

Always utilize the available go-boost MCP tools:
- Call 'app_info' at the start of new tasks.
- Call 'ast_inspect' to view struct field tags and interface signatures.
- Call 'test_runner' to verify fixes with 'go test'.
- Call 'code_check' to run 'go vet' on modified packages.

Rules:
1. Follow standard Go idioms and naming conventions (no unnecessary getters, camelCase, short receiver names).
2. Propagate context.Context down the call chain.
3. Handle every error; wrap with '%%w'.
4. Write table-driven unit tests.
`, stack.ModuleName, stack.GoVersion, stack.Framework, stack.Architecture, stack.Framework, stack.ORM, stack.Logger)
}

func GenerateMCPConfigJSON(binaryPath string) string {
	if binaryPath == "" {
		binaryPath = "go-boost"
	}
	return fmt.Sprintf(`{
  "mcpServers": {
    "go-boost": {
      "command": "%s",
      "args": ["mcp"]
    }
  }
}
`, binaryPath)
}

var BuiltinSkills = map[string]string{
	"go-clean-architecture": `# Skill: Go Clean Architecture Pattern

Use this skill when developing or refactoring features in projects following Clean / Hexagonal Architecture in Go.

## Architectural Layers (Dependency Rule points inwards)
1. **Domain / Entity Layer** (\x60internal/domain\x60):
   - Pure business models and entities without external framework dependencies.
   - Domain errors and repository interface contracts.
2. **Repository Layer** (\x60internal/repository\x60):
   - Implements domain repository interfaces.
   - Manages database interactions (GORM, SQLX, Bun, standard sql).
   - Translates database records into domain entities.
3. **Usecase / Service Layer** (\x60internal/usecase\x60 or \x60internal/service\x60):
   - Business workflow orchestration and transaction management.
   - Independent of transport protocols (HTTP/gRPC/CLI).
4. **Delivery / Handler Layer** (\x60internal/handler\x60 or \x60internal/delivery\x60):
   - HTTP request parsing, DTO binding, and response serialization.
   - Calls usecases with \x60ctx\x60.
`,

	"go-table-tests": `# Skill: Go Table-Driven Tests Pattern

Use this skill when writing or refactoring unit and integration tests in Go.

## Standard Pattern
\x60\x60\x60go
func TestService_Action(t *testing.T) {
	tests := []struct {
		name      string
		input     InputDTO
		mockSetup func(m *MockRepo)
		want      *OutputDTO
		wantErr   bool
		errTarget error
	}{
		{
			name: "success case",
			input: InputDTO{ID: 1},
			mockSetup: func(m *MockRepo) {
				m.On("FindByID", mock.Anything, uint(1)).Return(&Entity{ID: 1}, nil)
			},
			want: &OutputDTO{ID: 1},
			wantErr: false,
		},
		{
			name: "not found case",
			input: InputDTO{ID: 99},
			mockSetup: func(m *MockRepo) {
				m.On("FindByID", mock.Anything, uint(99)).Return(nil, ErrNotFound)
			},
			want: nil,
			wantErr: true,
			errTarget: ErrNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
		})
	}
}
\x60\x60\x60
`,

	"go-concurrency-safety": `# Skill: Go Concurrency & Goroutine Safety

Use this skill when implementing concurrent code, worker pools, background tasks, or caching in Go.

## Rules & Patterns
1. **Never leak goroutines**: Every spawned goroutine must have a deterministic termination path via \x60ctx.Done()\x60 or channel close.
2. **Use errgroup for fan-out / fan-in**:
   \x60\x60\x60go
   g, ctx := errgroup.WithContext(ctx)
   for _, item := range items {
       item := item
       g.Go(func() error {
           return process(ctx, item)
       })
   }
   if err := g.Wait(); err != nil {
       return err
   }
   \x60\x60\x60
3. **Mutex Hygiene**: Always \x60defer mu.Unlock()\x60 immediately after \x60mu.Lock()\x60.
4. **Channel Ownership**: The sender/producer owns the channel and is the only one responsible for closing it.
`,

	"go-error-handling": `# Skill: Go Idiomatic Error Handling

Use this skill for error definitions, wrapping, and error matching.

## Guidelines
1. **Sentinel Errors**: Define sentinel errors as package-level variables:
   \x60\x60\x60go
   var ErrUserNotFound = errors.New("user not found")
   \x60\x60\x60
2. **Error Wrapping**: Wrap errors with context using \x60%w\x60:
   \x60\x60\x60go
   if err != nil {
       return fmt.Errorf("find user %d: %w", id, err)
   }
   \x60\x60\x60
3. **Error Matching**: Always use \x60errors.Is\x60 and \x60errors.As\x60:
   \x60\x60\x60go
   if errors.Is(err, ErrUserNotFound) { ... }
   \x60\x60\x60
`,
}
