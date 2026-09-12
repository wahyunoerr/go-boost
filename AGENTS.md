# Go Project Engineering Guidelines (go-boost)

This project `github.com/wahyunoerr/go-boost` is powered by **Go 1.26.5** and **go-boost**.

## Detected Stack Architecture
- **Framework**: `net/http` (v)
- **ORM / Database Layer**: `database/sql` (v)
- **Database Engine**: `unknown`
- **Logger**: `slog`
- **Architecture**: `standard_flat`

## Available MCP Tools via `go-boost`
You have access to the `go-boost` MCP server with 11 native tools:
1. `app_info`: Call upfront to see complete project metadata & packages.
2. `ast_inspect`: Deeply inspect Go structs, interfaces, and field tags (`json`, `gorm`, `validate`).
3. `route_list`: Statically view all registered HTTP endpoints and handlers.
4. `db_schema`: Inspect database tables, column types, keys, and foreign relations.
5. `db_query`: Execute safe, read-only SQL queries (`SELECT`, `SHOW`, `EXPLAIN`).
6. `db_connections`: Discover configured databases.
7. `read_logs`: Read and parse application logs (`slog`, `zap`, `zerolog`).
8. `last_error`: Get stack traces of recent backend errors or panics.
9. `test_runner`: Run targeted Go unit tests and view structured failure diagnostics.
10. `go_doc`: Check official Go stdlib or third-party symbol documentation.
11. `code_check`: Run `go vet` to ensure generated code compiles cleanly.

## Go Coding Standards & Best Practices
- **Context Propagation**: Always pass `ctx context.Context` as the very first parameter to I/O functions, handlers, and repositories.
- **Error Handling**:
  - Wrap errors with context using `fmt.Errorf("...: %w", err)`.
  - Inspect errors using `errors.Is(err, target)` and `errors.As(err, &target)`.
  - Never ignore returned errors with `_` unless explicitly justified.
- **Modern Go Idioms** (Go 1.22+):
  - Standard loop variable scoping (each iteration creates a new variable; no closure leaks).
  - Standard `slices` and `maps` packages over manual loops where readable.
  - Use structured logging via `log/slog`: `slog.InfoContext(ctx, "...", "key", val)`.
- **Testing**: Write idiomatic table-driven tests with `t.Run(tc.name, func(t *testing.T) { ... })`.
