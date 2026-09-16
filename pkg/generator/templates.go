package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wahyunoerr/go-boost/v2/pkg/detector"
)

const toolPackagePath = "github.com/wahyunoerr/go-boost/v2/cmd/go-boost"

const (
	guidelineBeginMarker = "<!-- BEGIN go-boost generated guidelines -->"
	guidelineEndMarker   = "<!-- END go-boost generated guidelines -->"
	rulesBeginMarker     = "# BEGIN go-boost generated rules"
	rulesEndMarker       = "# END go-boost generated rules"
)

type Skill struct {
	Name        string
	Description string
	Body        string
	AppliesTo   func(stack *detector.ProjectStack) bool
}

func (s Skill) Render() string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + s.Name + "\n")
	sb.WriteString("description: " + s.Description + "\n")
	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimRight(s.Body, "\n"))
	sb.WriteString("\n")
	return sb.String()
}

func GenerateAgentsMD(stack *detector.ProjectStack) string {
	return guidelineBeginMarker + "\n" + generateGuidelines(stack) + guidelineEndMarker + "\n"
}

func generateGuidelines(stack *detector.ProjectStack) string {
	var sb strings.Builder

	sb.WriteString("# Go Project Engineering Guidelines (go-boost)\n\n")
	sb.WriteString(fmt.Sprintf("This project `%s` is powered by **Go %s** and **go-boost**.\n\n", stack.ModuleName, stack.GoVersion))

	sb.WriteString("## Detected Stack Architecture\n")
	sb.WriteString(fmt.Sprintf("- **Framework**: %s\n", formatDetected(stack.Framework, stack.FrameworkVersion)))
	sb.WriteString(fmt.Sprintf("- **ORM / Database Layer**: %s\n", formatDetected(stack.ORM, stack.ORMVersion)))
	sb.WriteString(fmt.Sprintf("- **Database Engine**: %s\n", formatDetected(stack.DatabaseEngine, "")))
	if isDetected(stack.CacheEngine) {
		sb.WriteString(fmt.Sprintf("- **Cache Engine**: `%s`\n", stack.CacheEngine))
	}
	if isDetected(stack.QueueEngine) {
		sb.WriteString(fmt.Sprintf("- **Queue Engine**: `%s`\n", stack.QueueEngine))
	}
	if isDetected(stack.RPC) {
		sb.WriteString(fmt.Sprintf("- **RPC**: `%s`\n", stack.RPC))
	}
	if isDetected(stack.ConfigManager) {
		sb.WriteString(fmt.Sprintf("- **Config Manager**: `%s`\n", stack.ConfigManager))
	}
	sb.WriteString(fmt.Sprintf("- **Logger**: %s\n", formatDetected(stack.Logger, "")))
	sb.WriteString(fmt.Sprintf("- **Architecture**: `%s`\n\n", stack.Architecture))

	sb.WriteString(renderLayout(stack.Layout))

	sb.WriteString("## Available MCP Tools via `go-boost`\n")
	sb.WriteString(fmt.Sprintf("You have access to the `go-boost` MCP server with %d native tools:\n", len(ToolCatalog)))
	for i, tool := range ToolCatalog {
		sb.WriteString(fmt.Sprintf("%d. `%s`: %s\n", i+1, tool.Name, tool.Summary))
	}
	sb.WriteString("\n")

	sb.WriteString("## Go Coding Standards & Best Practices\n")
	sb.WriteString("- **Context Propagation**: Always pass `ctx context.Context` as the very first parameter to I/O functions, handlers, and repositories.\n")
	sb.WriteString("- **Error Handling**:\n")
	sb.WriteString("  - Wrap errors with context using `fmt.Errorf(\"...: %w\", err)`.\n")
	sb.WriteString("  - Inspect errors using `errors.Is(err, target)` and `errors.As(err, &target)`.\n")
	sb.WriteString("  - Never ignore returned errors with `_` unless explicitly justified.\n")
	sb.WriteString("- **Modern Go Idioms** (Go 1.22+):\n")
	sb.WriteString("  - Loop variables are scoped per iteration; do not write `x := x` copies.\n")
	sb.WriteString("  - Prefer the standard `slices` and `maps` packages over hand-written loops where readable.\n")
	switch stack.Logger {
	case "zap":
		sb.WriteString("  - Use Uber Zap structured logging: `logger.Info(\"...\", zap.String(\"key\", val))`.\n")
	case "zerolog":
		sb.WriteString("  - Use zerolog structured logging: `log.Info().Str(\"key\", val).Msg(\"...\")`.\n")
	case "logrus":
		sb.WriteString("  - Use logrus structured logging: `log.WithField(\"key\", val).Info(\"...\")`.\n")
	default:
		sb.WriteString("  - Use structured logging via `log/slog`: `slog.InfoContext(ctx, \"...\", \"key\", val)`.\n")
	}
	sb.WriteString("- **Testing**: Write idiomatic table-driven tests with `t.Run(tc.name, func(t *testing.T) { ... })`.\n")

	return sb.String()
}

func renderLayout(layout *detector.ProjectLayout) string {
	if layout == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Layout\n")
	sb.WriteString("These are the directories that exist in this repository. Put new code in the directory that already holds code of the same kind.\n\n")

	if len(layout.MainPackages) > 0 {
		sb.WriteString(fmt.Sprintf("- **Entry points**: %s\n", codeList(layout.MainPackages)))
	}
	if len(layout.SourceRoots) > 0 {
		sb.WriteString(fmt.Sprintf("- **Source roots**: %s\n", codeList(layout.SourceRoots)))
	}

	for _, layer := range []string{"handler", "usecase", "repository", "domain", "transport", "infra", "middleware"} {
		paths, ok := layout.Layers[layer]
		if !ok || len(paths) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", layerLabel(layer), codeList(paths)))
	}

	if len(layout.Features) > 0 {
		sb.WriteString(fmt.Sprintf("- **Feature modules**: %s\n", codeList(layout.Features)))
	}
	if len(layout.ConfigDirs) > 0 {
		sb.WriteString(fmt.Sprintf("- **Configuration**: %s\n", codeList(layout.ConfigDirs)))
	}
	if len(layout.Migrations) > 0 {
		sb.WriteString(fmt.Sprintf("- **Migrations**: %s\n", codeList(layout.Migrations)))
	}
	if layout.TestStyle != "" {
		sb.WriteString(fmt.Sprintf("- **Test package style**: %s\n", layout.TestStyle))
	}

	sb.WriteString("\n")
	sb.WriteString(layoutGuidance(layout.Style))
	sb.WriteString("\n")

	return sb.String()
}

func layerLabel(layer string) string {
	switch layer {
	case "handler":
		return "HTTP handlers"
	case "usecase":
		return "Business logic"
	case "repository":
		return "Data access"
	case "domain":
		return "Domain types"
	case "transport":
		return "Other transports"
	case "infra":
		return "Infrastructure"
	case "middleware":
		return "Middleware"
	default:
		return layer
	}
}

func layoutGuidance(style string) string {
	switch style {
	case "feature":
		return "This project groups code by feature. A new feature gets its own directory containing every layer it needs; do not add a shared top-level layer directory.\n"
	case "layered":
		return "This project groups code by layer. Follow the existing dependency direction and do not let an inner layer import an outer one.\n"
	case "partial":
		return "This project uses some layer directories but not a full set. Follow the local convention of the package you are editing rather than introducing a new structure.\n"
	default:
		return "This project keeps its packages flat. Do not introduce a layered directory tree unless asked.\n"
	}
}

func codeList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+item+"`")
	}
	return strings.Join(quoted, ", ")
}

func isDetected(value string) bool {
	return value != "" && value != "none" && value != "unknown"
}

func formatDetected(value, version string) string {
	if !isDetected(value) {
		return "`" + value + "` (not detected in go.mod; assumed default)"
	}
	if version != "" {
		return fmt.Sprintf("`%s` (%s)", value, version)
	}
	return "`" + value + "`"
}

func GenerateClaudeMD(stack *detector.ProjectStack) string {
	return GenerateAgentsMD(stack)
}

func GenerateCursorRules(stack *detector.ProjectStack) string {
	body := fmt.Sprintf(`Cursor Rules: %s (Go %s)
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

	return rulesBeginMarker + "\n" + body + rulesEndMarker + "\n"
}

func GenerateMCPConfigJSON(binaryPath string) string {
	return renderMCPConfig("mcpServers", MCPInvocation{Command: binaryPath, Args: []string{"mcp"}})
}

func GenerateVSCodeMCPConfigJSON(binaryPath string) string {
	return renderMCPConfig("servers", MCPInvocation{Command: binaryPath, Args: []string{"mcp"}})
}

func renderMCPConfig(key string, invocation MCPInvocation) string {
	config := map[string]any{
		key: map[string]any{
			serverKey: invocation.entry(),
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data) + "\n"
}

type MCPInvocation struct {
	Command  string
	Args     []string
	Portable bool
}

func (m MCPInvocation) entry() map[string]any {
	command := m.Command
	if command == "" {
		command = "go-boost"
	}
	args := m.Args
	if len(args) == 0 {
		args = []string{"mcp"}
	}
	return map[string]any{
		"command": command,
		"args":    args,
	}
}

func ResolveMCPInvocation(rootDir, binaryPath string) MCPInvocation {
	if hasToolDirective(rootDir) {
		return MCPInvocation{Command: "go", Args: []string{"tool", "go-boost", "mcp"}, Portable: true}
	}
	if onPath("go-boost") {
		return MCPInvocation{Command: "go-boost", Args: []string{"mcp"}, Portable: true}
	}
	if binaryPath == "" {
		binaryPath = "go-boost"
	}
	return MCPInvocation{Command: binaryPath, Args: []string{"mcp"}}
}

func hasToolDirective(rootDir string) bool {
	data, err := os.ReadFile(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "tool ") && strings.Contains(line, toolPackagePath) {
			return true
		}
		if line == toolPackagePath {
			return true
		}
	}
	return false
}

func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func GenerateMakefile(mainPackage string) string {
	if mainPackage == "" {
		mainPackage = "."
	}
	return fmt.Sprintf(`.PHONY: run test check bench mcp

run:
	@go-boost run go run %s

test:
	@go test ./...

check:
	@go-boost check

bench:
	@go-boost bench

mcp:
	@go-boost mcp
`, mainPackage)
}

type ToolSummary struct {
	Name    string
	Summary string
}

var ToolCatalog = []ToolSummary{
	{"app_info", "Call upfront to see complete project metadata and packages."},
	{"ast_inspect", "Inspect Go structs, interfaces, and field tags (`json`, `gorm`, `db`, `validate`)."},
	{"route_list", "List every registered HTTP endpoint with its handler and source location."},
	{"db_connections", "Discover configured databases. Passwords are never returned."},
	{"db_schema", "Inspect tables, columns, indexes, and foreign keys."},
	{"db_query", "Run a read-only SQL query. The database is opened read-only, so writes cannot succeed."},
	{"read_logs", "Read and parse application logs (`slog`, `zap`, `zerolog`, logfmt)."},
	{"last_error", "Get the most recent backend error or panic with its stack trace."},
	{"test_runner", "Run targeted Go tests and get structured failure diagnostics."},
	{"go_doc", "Look up Go stdlib or third-party symbol documentation."},
	{"code_check", "Run `go vet` to verify code compiles cleanly."},
	{"find_implementations", "Find which structs implement a given interface."},
	{"concurrency_check", "Detect context leaks, mutex leaks, and unsupervised goroutines."},
	{"schema_struct_diff", "Compare database columns against Go struct tags."},
	{"bench_runner", "Run benchmarks with allocation profiling."},
	{"security_scan", "Detect SQL injection, hardcoded secrets, command injection, and insecure TLS."},
	{"mock_generate", "Generate a compilable pure-Go mock for an interface."},
	{"openapi_generate", "Generate an OpenAPI 3.0 specification from discovered routes."},
	{"migration_generate", "Generate SQL migrations from schema and struct differences."},
	{"deadcode_detect", "Detect unreferenced structs, functions, and interfaces."},
	{"diagnose_run", "Run a command under supervision and diagnose crashes, with a timeout."},
}

var BuiltinSkills = []Skill{
	{
		Name:        "go-error-handling",
		Description: "Define, wrap, and match errors idiomatically in Go, including sentinel errors and custom error types.",
		AppliesTo:   func(stack *detector.ProjectStack) bool { return true },
		Body: "# Go Idiomatic Error Handling\n\n" +
			"## When to use this skill\n" +
			"Use this skill when defining errors, wrapping them across layers, or deciding how a caller should inspect a failure.\n\n" +
			"## Guidelines\n" +
			"1. **Sentinel errors** are package-level variables:\n" +
			"```go\n" +
			"var ErrUserNotFound = errors.New(\"user not found\")\n" +
			"```\n" +
			"2. **Wrap with context** using `%w` so the chain stays inspectable:\n" +
			"```go\n" +
			"if err != nil {\n" +
			"    return fmt.Errorf(\"find user %d: %w\", id, err)\n" +
			"}\n" +
			"```\n" +
			"3. **Match with `errors.Is` and `errors.As`**, never by comparing strings:\n" +
			"```go\n" +
			"if errors.Is(err, ErrUserNotFound) {\n" +
			"    return status.NotFound()\n" +
			"}\n\n" +
			"var validationErr *ValidationError\n" +
			"if errors.As(err, &validationErr) {\n" +
			"    return status.BadRequest(validationErr.Field)\n" +
			"}\n" +
			"```\n" +
			"4. **Do not log and return** the same error; pick one so it is reported once.\n" +
			"5. **Never discard an error** with `_` unless you write down why it is safe.\n",
	},
	{
		Name:        "go-table-tests",
		Description: "Write table-driven Go tests with subtests, including error cases and mock setup.",
		AppliesTo:   func(stack *detector.ProjectStack) bool { return true },
		Body: "# Go Table-Driven Tests\n\n" +
			"## When to use this skill\n" +
			"Use this skill when writing or refactoring unit and integration tests.\n\n" +
			"## Standard pattern\n" +
			"```go\n" +
			"func TestService_Action(t *testing.T) {\n" +
			"\ttests := []struct {\n" +
			"\t\tname      string\n" +
			"\t\tinput     InputDTO\n" +
			"\t\tsetupMock func(m *MockRepo)\n" +
			"\t\twant      *OutputDTO\n" +
			"\t\twantErr   error\n" +
			"\t}{\n" +
			"\t\t{\n" +
			"\t\t\tname:  \"returns the stored entity\",\n" +
			"\t\t\tinput: InputDTO{ID: 1},\n" +
			"\t\t\tsetupMock: func(m *MockRepo) {\n" +
			"\t\t\t\tm.FindByIDFunc = func(ctx context.Context, id uint) (*Entity, error) {\n" +
			"\t\t\t\t\treturn &Entity{ID: 1}, nil\n" +
			"\t\t\t\t}\n" +
			"\t\t\t},\n" +
			"\t\t\twant: &OutputDTO{ID: 1},\n" +
			"\t\t},\n" +
			"\t\t{\n" +
			"\t\t\tname:  \"propagates a not-found error\",\n" +
			"\t\t\tinput: InputDTO{ID: 99},\n" +
			"\t\t\tsetupMock: func(m *MockRepo) {\n" +
			"\t\t\t\tm.FindByIDFunc = func(ctx context.Context, id uint) (*Entity, error) {\n" +
			"\t\t\t\t\treturn nil, ErrNotFound\n" +
			"\t\t\t\t}\n" +
			"\t\t\t},\n" +
			"\t\t\twantErr: ErrNotFound,\n" +
			"\t\t},\n" +
			"\t}\n\n" +
			"\tfor _, tc := range tests {\n" +
			"\t\tt.Run(tc.name, func(t *testing.T) {\n" +
			"\t\t\trepo := NewMockRepo()\n" +
			"\t\t\tif tc.setupMock != nil {\n" +
			"\t\t\t\ttc.setupMock(repo)\n" +
			"\t\t\t}\n\n" +
			"\t\t\tgot, err := NewService(repo).Action(context.Background(), tc.input)\n" +
			"\t\t\tif !errors.Is(err, tc.wantErr) {\n" +
			"\t\t\t\tt.Fatalf(\"err = %v, want %v\", err, tc.wantErr)\n" +
			"\t\t\t}\n" +
			"\t\t\tif diff := cmp.Diff(tc.want, got); diff != \"\" {\n" +
			"\t\t\t\tt.Errorf(\"unexpected result (-want +got):\\n%s\", diff)\n" +
			"\t\t\t}\n" +
			"\t\t})\n" +
			"\t}\n" +
			"}\n" +
			"```\n\n" +
			"## Rules\n" +
			"- Name each case after the behaviour it proves, not after the input.\n" +
			"- Always assert the error, not only the happy path value.\n" +
			"- Use `t.Parallel()` only when the cases share no mutable state.\n" +
			"- Generate mocks with the go-boost `mock_generate` tool.\n",
	},
	{
		Name:        "go-concurrency-safety",
		Description: "Write goroutines, worker pools, and shared state in Go without leaks or data races.",
		AppliesTo:   func(stack *detector.ProjectStack) bool { return true },
		Body: "# Go Concurrency and Goroutine Safety\n\n" +
			"## When to use this skill\n" +
			"Use this skill when spawning goroutines, building worker pools, or sharing state between them.\n\n" +
			"## Rules\n" +
			"1. **Every goroutine needs a termination path** through `ctx.Done()` or a closed channel. A goroutine with no exit is a leak.\n" +
			"2. **Use errgroup for fan-out** so failures propagate and the caller can wait:\n" +
			"```go\n" +
			"g, ctx := errgroup.WithContext(ctx)\n" +
			"for _, item := range items {\n" +
			"    g.Go(func() error {\n" +
			"        return process(ctx, item)\n" +
			"    })\n" +
			"}\n" +
			"if err := g.Wait(); err != nil {\n" +
			"    return err\n" +
			"}\n" +
			"```\n" +
			"Since Go 1.22 each iteration has its own loop variable, so no `item := item` copy is needed.\n" +
			"3. **Pair every `Lock` with a deferred `Unlock`** on the next line. A manual `Unlock` is skipped by every early return:\n" +
			"```go\n" +
			"mu.Lock()\n" +
			"defer mu.Unlock()\n" +
			"```\n" +
			"4. **The sender owns the channel** and is the only party that closes it.\n" +
			"5. **Always `defer cancel()`** after `context.WithTimeout` or `context.WithCancel`, or the timer leaks until it fires.\n" +
			"6. **Run tests with `-race`** before trusting concurrent code.\n\n" +
			"## Verification\n" +
			"Run the go-boost `concurrency_check` tool to find context leaks, mutex leaks, and unsupervised goroutines.\n",
	},
	{
		Name:        "go-clean-architecture",
		Description: "Structure features across domain, repository, usecase, and delivery layers in a Clean or Hexagonal Go codebase.",
		AppliesTo: func(stack *detector.ProjectStack) bool {
			return stack.Architecture == "clean_architecture" || stack.Architecture == "hexagonal"
		},
		Body: "# Go Clean Architecture\n\n" +
			"## When to use this skill\n" +
			"Use this skill when adding or refactoring a feature in a project that follows Clean or Hexagonal Architecture.\n\n" +
			"## Layers, with dependencies pointing inwards\n" +
			"1. **Domain** (`internal/domain`): business entities, sentinel errors, and the repository interfaces the inner layers own. No framework imports.\n" +
			"2. **Repository** (`internal/repository`): implements the domain interfaces, talks to the database, and maps rows to entities.\n" +
			"3. **Usecase** (`internal/usecase` or `internal/service`): orchestrates business rules and transaction boundaries. Knows nothing about HTTP or gRPC.\n" +
			"4. **Delivery** (`internal/handler` or `internal/delivery`): parses requests into DTOs, calls a usecase with `ctx`, and serialises the response.\n\n" +
			"## Rules\n" +
			"- The interface belongs to the layer that consumes it, not the one that implements it.\n" +
			"- A domain entity never carries `json` or `gorm` tags; map to a DTO or a persistence model instead.\n" +
			"- A handler never touches the database directly.\n" +
			"- Pass `ctx context.Context` as the first argument across every boundary.\n\n" +
			"## Verification\n" +
			"Run the go-boost `find_implementations` tool to confirm a struct satisfies the domain interface it claims to.\n",
	},
}

func SkillsFor(stack *detector.ProjectStack) []Skill {
	var selected []Skill
	for _, skill := range BuiltinSkills {
		if skill.AppliesTo == nil || skill.AppliesTo(stack) {
			selected = append(selected, skill)
		}
	}
	return selected
}
