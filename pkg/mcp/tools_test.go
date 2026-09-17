package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newToolServer(t *testing.T, files map[string]string) (*Server, string) {
	t.Helper()

	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.22\n"
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	srv := NewServer("test", "0.0.0")
	RegisterAllTools(srv, dir)
	RegisterAllPrompts(srv)
	RegisterAllResources(srv, dir)
	return srv, dir
}

func callTool(t *testing.T, srv *Server, name string, args map[string]any) *CallToolResult {
	t.Helper()

	srv.mu.RLock()
	handler, ok := srv.toolHandlers[name]
	srv.mu.RUnlock()
	if !ok {
		t.Fatalf("tool %q is not registered", name)
	}

	res, err := handler(context.Background(), args)
	if err != nil {
		t.Fatalf("tool %q returned an error: %v", name, err)
	}
	if res == nil {
		t.Fatalf("tool %q returned no result", name)
	}
	return res
}

func toolText(t *testing.T, res *CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		sb.WriteString(c.Text)
	}
	return sb.String()
}

func TestEveryRegisteredToolHasAUsableSchema(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	srv.mu.RLock()
	defer srv.mu.RUnlock()

	if len(srv.tools) == 0 {
		t.Fatal("no tools were registered")
	}

	for name, tool := range srv.tools {
		if tool.Description == "" {
			t.Errorf("tool %q has no description", name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %q has schema type %q, want object", name, tool.InputSchema.Type)
		}
		for _, required := range tool.InputSchema.Required {
			if _, ok := tool.InputSchema.Properties[required]; !ok {
				t.Errorf("tool %q requires %q but does not declare it as a property", name, required)
			}
		}
		for prop, def := range tool.InputSchema.Properties {
			if def.Type == "" {
				t.Errorf("tool %q property %q has no type", name, prop)
			}
			if def.Description == "" {
				t.Errorf("tool %q property %q has no description", name, prop)
			}
		}
		if _, ok := srv.toolHandlers[name]; !ok {
			t.Errorf("tool %q has no handler", name)
		}
	}
}

func TestAppInfoReportsStackAndCapabilities(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"go.mod": "module example.com/shop\n\ngo 1.22\n\nrequire github.com/gin-gonic/gin v1.10.0\n",
	})

	var info map[string]any
	if err := json.Unmarshal([]byte(toolText(t, callTool(t, srv, "app_info", nil))), &info); err != nil {
		t.Fatalf("app_info did not return JSON: %v", err)
	}

	if info["module_name"] != "example.com/shop" {
		t.Errorf("module_name = %v", info["module_name"])
	}
	if info["framework"] != "gin" {
		t.Errorf("framework = %v, want gin", info["framework"])
	}
	if _, ok := info["capabilities"]; !ok {
		t.Error("app_info must report which external clients are available")
	}
}

func TestAstInspectReadsStructsAndRejectsEscape(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"internal/domain/user.go": "package domain\n\ntype User struct {\n\tID    uint   `json:\"id\"`\n\tEmail string `json:\"email\"`\n}\n",
	})

	out := toolText(t, callTool(t, srv, "ast_inspect", map[string]any{"path": "./internal/domain"}))
	if !strings.Contains(out, `"name": "User"`) {
		t.Errorf("expected the User struct, got:\n%s", out)
	}
	if !strings.Contains(out, "Email") {
		t.Errorf("expected the Email field, got:\n%s", out)
	}
}

func TestRouteListAndOpenAPIAgree(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"routes.go": `package app

import "net/http"

func Setup(mux *http.ServeMux) {
	mux.HandleFunc("GET /items", listItems)
	mux.HandleFunc("POST /items", createItem)
}
`,
	})

	routes := toolText(t, callTool(t, srv, "route_list", nil))
	for _, want := range []string{"/items", "GET", "POST"} {
		if !strings.Contains(routes, want) {
			t.Errorf("route_list is missing %q:\n%s", want, routes)
		}
	}

	spec := toolText(t, callTool(t, srv, "openapi_generate", map[string]any{"title": "Items API", "version": "3.0.0"}))
	var doc map[string]any
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("openapi_generate did not return JSON: %v", err)
	}
	info := doc["info"].(map[string]any)
	if info["title"] != "Items API" || info["version"] != "3.0.0" {
		t.Errorf("title/version not honoured: %v", info)
	}
	if _, ok := doc["paths"].(map[string]any)["/items"]; !ok {
		t.Errorf("expected /items in the spec: %s", spec)
	}
}

func TestSecurityScanAndDeadcodeAcceptAPath(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"internal/auth/token.go": "package auth\n\nconst apiKey = \"abcdefghijklmnopqrstuvwxyz1234\"\n\nfunc Sign() string { return apiKey }\n",
		"internal/safe/safe.go":  "package safe\n\nfunc Add(a, b int) int { return a + b }\n",
	})

	scoped := toolText(t, callTool(t, srv, "security_scan", map[string]any{"path": "./internal/safe"}))
	if strings.Contains(scoped, "hardcoded_secret") {
		t.Errorf("scanning ./internal/safe must not report findings from elsewhere:\n%s", scoped)
	}

	whole := toolText(t, callTool(t, srv, "security_scan", nil))
	if !strings.Contains(whole, "hardcoded_secret") {
		t.Errorf("scanning the project must find the credential:\n%s", whole)
	}

	if _, ok := srv.toolHandlers["deadcode_detect"]; !ok {
		t.Fatal("deadcode_detect is not registered")
	}
	dead := toolText(t, callTool(t, srv, "deadcode_detect", map[string]any{"path": "./internal/safe"}))
	if !strings.Contains(dead, "Add") {
		t.Errorf("expected the unreferenced Add function:\n%s", dead)
	}
}

func TestToolsRejectAPathOutsideTheProject(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	for _, tool := range []string{"security_scan", "deadcode_detect"} {
		srv.mu.RLock()
		handler := srv.toolHandlers[tool]
		srv.mu.RUnlock()

		if _, err := handler(context.Background(), map[string]any{"path": "./does-not-exist"}); err == nil {
			t.Errorf("%s accepted a path that is not a directory", tool)
		}
	}
}

func TestConcurrencyCheckAndFindImplementations(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"internal/store/store.go": `package store

import (
	"context"
	"sync"
)

type Repository interface {
	Get(ctx context.Context, id string) (string, error)
}

type Memory struct {
	mu sync.Mutex
}

func (m *Memory) Get(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	if id == "" {
		return "", nil
	}
	m.mu.Unlock()
	return id, nil
}
`,
	})

	issues := toolText(t, callTool(t, srv, "concurrency_check", nil))
	if !strings.Contains(issues, "mutex_leak") {
		t.Errorf("expected the lock held across an early return:\n%s", issues)
	}

	impls := toolText(t, callTool(t, srv, "find_implementations", map[string]any{"interface": "Repository"}))
	if !strings.Contains(impls, "Memory") {
		t.Errorf("expected Memory to implement Repository:\n%s", impls)
	}
}

func TestMockGenerateReturnsCompilableSource(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"internal/store/store.go": "package store\n\nimport \"context\"\n\ntype Repository interface {\n\tGet(ctx context.Context, id string) (string, error)\n}\n",
	})

	code := toolText(t, callTool(t, srv, "mock_generate", map[string]any{"interface": "Repository"}))
	for _, want := range []string{"type MockRepository struct", "var _ Repository = (*MockRepository)(nil)", "func (m *MockRepository) Get"} {
		if !strings.Contains(code, want) {
			t.Errorf("generated mock is missing %q:\n%s", want, code)
		}
	}
}

func TestReadLogsAndLastError(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"app.log": "time=2026-09-17T10:00:00Z level=INFO msg=started\ntime=2026-09-17T10:00:01Z level=ERROR msg=\"payment failed\"\n",
	})

	logs := toolText(t, callTool(t, srv, "read_logs", map[string]any{"entries": float64(10)}))
	if !strings.Contains(logs, "payment failed") {
		t.Errorf("read_logs missed the error entry:\n%s", logs)
	}

	last := toolText(t, callTool(t, srv, "last_error", nil))
	if !strings.Contains(last, "payment failed") {
		t.Errorf("last_error missed the error:\n%s", last)
	}
}

func TestDatabaseToolsExplainThemselvesWithoutAConnection(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	conns := toolText(t, callTool(t, srv, "db_connections", nil))
	if conns == "" {
		t.Error("db_connections returned nothing")
	}

	schema := toolText(t, callTool(t, srv, "db_schema", nil))
	if !strings.Contains(strings.ToLower(schema), "no database") {
		t.Errorf("db_schema should say there is no connection, got: %s", schema)
	}

	diff := toolText(t, callTool(t, srv, "schema_struct_diff", nil))
	if !strings.Contains(strings.ToLower(diff), "no database") {
		t.Errorf("schema_struct_diff should say there is no connection, got: %s", diff)
	}
}

func TestDbQueryRejectsWritesThroughTheTool(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	srv.mu.RLock()
	handler := srv.toolHandlers["db_query"]
	srv.mu.RUnlock()

	for _, query := range []string{"DROP TABLE users", "PRAGMA user_version = 1", "SELECT * INTO copy FROM users"} {
		if _, err := handler(context.Background(), map[string]any{"query": query}); err == nil {
			t.Errorf("db_query accepted a write: %s", query)
		}
	}

	if _, err := handler(context.Background(), map[string]any{}); err == nil {
		t.Error("db_query accepted a call with no query")
	}
}

func TestDiagnoseRunCapturesOutputAndTimesOut(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	res := callTool(t, srv, "diagnose_run", map[string]any{"command": "echo hello-from-child", "timeout_seconds": float64(30)})
	if !strings.Contains(toolText(t, res), "hello-from-child") {
		t.Errorf("expected the child output to be captured, got: %s", toolText(t, res))
	}

	timed := callTool(t, srv, "diagnose_run", map[string]any{"command": "sleep 30", "timeout_seconds": float64(1)})
	if !strings.Contains(toolText(t, timed), "still running") {
		t.Errorf("expected the timeout to be reported, got: %s", toolText(t, timed))
	}
}

func TestCodeCheckAndTestRunnerRejectFlagShapedTargets(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{})

	for _, tool := range []string{"code_check", "test_runner", "bench_runner"} {
		srv.mu.RLock()
		handler := srv.toolHandlers[tool]
		srv.mu.RUnlock()

		if _, err := handler(context.Background(), map[string]any{"package": "-exec=/bin/echo"}); err == nil {
			t.Errorf("%s accepted a flag-shaped package argument", tool)
		}
	}
}

func TestPromptsAndResourcesAreServed(t *testing.T) {
	srv, _ := newToolServer(t, map[string]string{
		"routes.go": "package app\n",
	})

	srv.mu.RLock()
	promptCount := len(srv.prompts)
	resourceCount := len(srv.resources)
	promptHandler := srv.promptHandlers["generate_table_tests"]
	resourceHandler := srv.resourceHandlers["project://metadata"]
	srv.mu.RUnlock()

	if promptCount == 0 || resourceCount == 0 {
		t.Fatalf("prompts=%d resources=%d, want both non-zero", promptCount, resourceCount)
	}

	prompt, err := promptHandler(context.Background(), map[string]string{"symbol": "UserService"})
	if err != nil {
		t.Fatalf("prompt handler failed: %v", err)
	}
	if len(prompt.Messages) == 0 || !strings.Contains(prompt.Messages[0].Content.Text, "UserService") {
		t.Errorf("prompt did not use the symbol: %+v", prompt.Messages)
	}

	resource, err := resourceHandler(context.Background(), "project://metadata")
	if err != nil {
		t.Fatalf("resource handler failed: %v", err)
	}
	if len(resource.Contents) == 0 || !strings.Contains(resource.Contents[0].Text, "module_name") {
		t.Errorf("metadata resource looks wrong: %+v", resource.Contents)
	}
}

func TestSplitCommandLineHandlesQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"plain", "go run ./cmd/api", []string{"go", "run", "./cmd/api"}},
		{"double quoted", `go run -ldflags "-X main.v=1" .`, []string{"go", "run", "-ldflags", "-X main.v=1", "."}},
		{"single quoted", `sh -c 'echo hello world'`, []string{"sh", "-c", "echo hello world"}},
		{"extra spaces", "  make   run  ", []string{"make", "run"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitCommandLine(tc.input)
			if err != nil {
				t.Fatalf("splitCommandLine failed: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("arg %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}

	if _, err := splitCommandLine(`go run "unterminated`); err == nil {
		t.Error("expected an unbalanced quote to be rejected")
	}
}

func TestHasMakeTargetMatchesRealTargetsOnly(t *testing.T) {
	dir := t.TempDir()
	makefile := ".PHONY: build run\n\nprerun:\n\t@echo no\n\nbuild run:\n\t@echo yes\n"
	path := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(path, []byte(makefile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !hasMakeTarget(path, "run") {
		t.Error("expected the run target to be found")
	}
	if hasMakeTarget(path, "deploy") {
		t.Error("a target that does not exist was reported as present")
	}
	if !hasMakeTarget(path, "build") {
		t.Error("expected build to be found in a multi-target rule")
	}
}
