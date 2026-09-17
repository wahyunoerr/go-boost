# Contributing

## Getting set up

```bash
git clone https://github.com/wahyunoerr/go-boost.git
cd go-boost
go build ./...
go test ./...
```

Go 1.22 or newer is required. There are no module dependencies to fetch.

## Before you open a pull request

CI runs these, so run them first:

```bash
gofmt -l ./cmd ./pkg      # must print nothing
go vet ./...
go test -race ./...
go build -o bin/go-boost ./cmd/go-boost
./bin/go-boost security   # go-boost audits itself
./bin/go-boost check
```

Coverage must stay at or above 60%.

## House style

**No comments.** The codebase carries none, and CI does not enforce that, so please keep it that way. Name things so the comment is unnecessary. The only exception is a `//go:build` directive.

**Tests state the behaviour, not the implementation.** A test name should describe what must be true. Prefer table-driven tests with `t.Run`.

**A bug fix comes with a test that fails without it.** Several tools in this repository once reported clean results on code that was clearly broken; the tests exist so that cannot happen quietly again.

## Adding an MCP tool

Register it in `pkg/mcp/tools.go` and add it to `ToolCatalog` in `pkg/generator/templates.go`, which is what generates the tool list in the guidelines. `TestGeneratedGuidelinesListEveryTool` fails if you forget the second step.

Give every parameter a type and a description. `TestEveryRegisteredToolHasAUsableSchema` checks this.

If the tool accepts a package or path argument that reaches a `go` subcommand, validate it with `validateGoTarget`. `go test`, `go vet`, and `go doc` all accept flags that execute an arbitrary binary.

## Reporting a security issue

See [SECURITY.md](SECURITY.md). Do not open a public issue for a vulnerability.
