package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wahyunoerr/go-boost/v2/pkg/detector"
)

func initProject(t *testing.T, files map[string]string) (string, *InitResult) {
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

	res, err := GenerateProjectArtifacts(dir, "/usr/local/bin/go-boost")
	if err != nil {
		t.Fatalf("GenerateProjectArtifacts failed: %v", err)
	}
	return dir, res
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestGenerateProjectArtifacts(t *testing.T) {
	dir, res := initProject(t, map[string]string{})

	for _, name := range []string{".mcp.json", filepath.Join(".cursor", "mcp.json"), filepath.Join(".vscode", "mcp.json"), filepath.Join(".gemini", "settings.json")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to be created: %v", name, err)
		}
	}

	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", "Makefile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("expected %s NOT to be created by default", name)
		}
	}

	if len(res.GeneratedFiles) == 0 {
		t.Error("expected generated files to be reported")
	}
}

func TestInitPreservesExistingUserContent(t *testing.T) {
	dir, _ := initProject(t, map[string]string{
		".mcp.json": `{"mcpServers":{"github":{"command":"gh-mcp","args":["serve"]}}}`,
	})

	var config map[string]map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".mcp.json"))), &config); err != nil {
		t.Fatalf("generated .mcp.json is not valid JSON: %v", err)
	}
	if _, ok := config["mcpServers"]["github"]; !ok {
		t.Error("an existing MCP server entry was removed")
	}
	if _, ok := config["mcpServers"]["go-boost"]; !ok {
		t.Error("the go-boost MCP server was not added")
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dir, _ := initProject(t, map[string]string{})
	before := readFile(t, filepath.Join(dir, ".mcp.json"))

	for i := 0; i < 3; i++ {
		if _, err := GenerateProjectArtifacts(dir, "/usr/local/bin/go-boost"); err != nil {
			t.Fatalf("GenerateProjectArtifacts failed: %v", err)
		}
	}

	after := readFile(t, filepath.Join(dir, ".mcp.json"))
	if before != after {
		t.Errorf("repeated runs changed the file:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestMCPConfigIsValidJSONForWindowsPaths(t *testing.T) {
	raw := GenerateMCPConfigJSON(`C:\Users\dev\go\bin\go-boost.exe`)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("generated config is not valid JSON:\n%s", raw)
	}

	var config map[string]map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got := config["mcpServers"]["go-boost"]["command"]; got != `C:\Users\dev\go\bin\go-boost.exe` {
		t.Errorf("command = %v, want the original Windows path", got)
	}
}

func TestVSCodeConfigUsesServersKey(t *testing.T) {
	dir, _ := initProject(t, map[string]string{})

	raw := readFile(t, filepath.Join(dir, ".vscode", "mcp.json"))
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := config["servers"]; !ok {
		t.Errorf("VS Code reads the 'servers' key, got: %s", raw)
	}
}

func TestBuiltinSkillsAreLockedByDefault(t *testing.T) {
	dir, res := initProject(t, map[string]string{
		filepath.Join("internal", "domain", "user.go"):     "package domain\n",
		filepath.Join("internal", "usecase", "user.go"):    "package usecase\n",
		filepath.Join("internal", "repository", "user.go"): "package repository\n",
	})

	if len(res.InstalledSkill) != 0 {
		t.Fatalf("expected 0 built-in skills installed by default, got %v", res.InstalledSkill)
	}

	for _, name := range []string{"go-error-handling", "go-table-tests", "go-concurrency-safety", "go-clean-architecture"} {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			t.Errorf("free built-in skill %q should be locked and not installed by default", name)
		}
	}
}

func TestCustomSkillsAreInstalledInAgentSkillFormat(t *testing.T) {
	paidSkill := "---\nname: go-boost-pro\ndescription: Official commercial Go architecture and MCP orchestration skill.\n---\n\n# Go-Boost Pro Skill\n\n```go\nmu.Lock()\ndefer mu.Unlock()\n```\n"
	dir, res := initProject(t, map[string]string{
		filepath.Join(".ai", "skills", "go-boost-pro", "SKILL.md"):   paidSkill,
		filepath.Join(".ai", "skills", "billing-domain", "SKILL.md"): "---\nname: billing-domain\ndescription: Invoicing rules.\n---\n\n# Billing\n",
	})

	if len(res.InstalledSkill) != 2 {
		t.Fatalf("expected 2 custom skills to be installed, got %v", res.InstalledSkill)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "go-boost-pro", "SKILL.md")
	body := readFile(t, skillPath)

	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("SKILL.md must start with YAML frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "name: go-boost-pro") {
		t.Errorf("frontmatter is missing the required name field:\n%s", body)
	}
	if !strings.Contains(body, "description: ") {
		t.Errorf("frontmatter is missing the required description field:\n%s", body)
	}
	if !strings.Contains(body, "```go") {
		t.Errorf("expected a real fenced code block in the skill body:\n%s", body)
	}

	added := readFile(t, filepath.Join(dir, ".claude", "skills", "billing-domain", "SKILL.md"))
	if !strings.Contains(added, "Billing") {
		t.Errorf("expected the project's own skill to be installed, got:\n%s", added)
	}
}

func TestGeneratedGuidelinesListEveryTool(t *testing.T) {
	stack := &detector.ProjectStack{ModuleName: "example.com/app", GoVersion: "1.24"}
	body := GenerateAgentsMD(stack)

	for _, tool := range ToolCatalog {
		if !strings.Contains(body, "`"+tool.Name+"`") {
			t.Errorf("generated guidelines omit the %q tool", tool.Name)
		}
	}
}

func TestGenerateMakefile(t *testing.T) {
	content := GenerateMakefile("./cmd/api")
	if !strings.Contains(content, "go run ./cmd/api") {
		t.Errorf("expected Makefile to contain entrypoint, got:\n%s", content)
	}
	defaultContent := GenerateMakefile("")
	if !strings.Contains(defaultContent, "go run .") {
		t.Errorf("expected default Makefile to contain '.', got:\n%s", defaultContent)
	}
}

func TestInitKeepsCrashReportsOutOfGit(t *testing.T) {
	dir, _ := initProject(t, map[string]string{
		".gitignore": "bin/\n*.log\n",
	})

	content := readFile(t, filepath.Join(dir, ".gitignore"))
	if !strings.Contains(content, ".go-boost/") {
		t.Errorf("expected .go-boost/ to be ignored, got:\n%s", content)
	}
	if !strings.Contains(content, "bin/") || !strings.Contains(content, "*.log") {
		t.Errorf("existing ignore rules were lost:\n%s", content)
	}

	if _, err := GenerateProjectArtifacts(dir, "/usr/local/bin/go-boost"); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if got := strings.Count(readFile(t, filepath.Join(dir, ".gitignore")), ".go-boost/"); got != 1 {
		t.Errorf("expected the rule to be added once, found %d times", got)
	}
}

func TestInitDoesNotCreateGitignoreWhereNoneExists(t *testing.T) {
	dir, _ := initProject(t, map[string]string{})

	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Error("go-boost should not introduce a .gitignore into a project that has none")
	}
}

func TestMCPConfigIsPortableWhenInstalledAsGoTool(t *testing.T) {
	dir, res := initProject(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.24\n\ntool github.com/wahyunoerr/go-boost/v2/cmd/go-boost\n",
	})

	if !res.PortableConfig {
		t.Error("a project with the tool directive should get a portable config")
	}

	raw := readFile(t, filepath.Join(dir, ".mcp.json"))
	if strings.Contains(raw, "/usr/local/bin/go-boost") {
		t.Errorf("config still hardcodes a machine-specific path:\n%s", raw)
	}

	var config map[string]map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	entry := config["mcpServers"]["go-boost"]
	if entry["command"] != "go" {
		t.Errorf("command = %v, want go", entry["command"])
	}
	args, _ := json.Marshal(entry["args"])
	if string(args) != `["tool","go-boost","mcp"]` {
		t.Errorf("args = %s, want [tool go-boost mcp]", args)
	}
}

func TestMCPConfigFallsBackToAbsolutePathWhenNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	dir, res := initProject(t, map[string]string{})

	if res.PortableConfig {
		t.Error("without the tool directive or a binary on PATH the config cannot be portable")
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, ".mcp.json")), "/usr/local/bin/go-boost") {
		t.Error("expected the absolute path fallback")
	}
}

func TestGuidelinesDescribeTheProjectsRealLayout(t *testing.T) {
	stack := &detector.ProjectStack{
		ModuleName: "example.com/app",
		GoVersion:  "1.24",
		Layout: &detector.ProjectLayout{
			Style:        "layered",
			MainPackages: []string{"./cmd/api", "./cmd/worker"},
			Layers: map[string][]string{
				"handler":    {"app/controllers"},
				"repository": {"app/storage"},
			},
			Migrations: []string{"migrations"},
		},
	}
	body := GenerateAgentsMD(stack)

	for _, want := range []string{
		"## Project Layout",
		"`./cmd/api`",
		"`./cmd/worker`",
		"`app/controllers`",
		"`app/storage`",
		"`migrations`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("generated guidelines omit %q:\n%s", want, body)
		}
	}
}

func TestGenerateClaudeAndCursorRules(t *testing.T) {
	stack := &detector.ProjectStack{ModuleName: "example.com/app", GoVersion: "1.24", Framework: "gin", ORM: "gorm", Architecture: "clean"}
	claude := GenerateClaudeMD(stack)
	if !strings.Contains(claude, "example.com/app") {
		t.Errorf("expected Claude MD to contain module name")
	}
	cursor := GenerateCursorRules(stack)
	if !strings.Contains(cursor, "example.com/app") || !strings.Contains(cursor, "gin") {
		t.Errorf("expected Cursor rules to contain module and framework")
	}
}

func TestGeminiConfigIsGeneratedWhereGeminiReadsIt(t *testing.T) {
	dir, _ := initProject(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.24\n\ntool github.com/wahyunoerr/go-boost/v2/cmd/go-boost\n",
	})

	raw := readFile(t, filepath.Join(dir, ".gemini", "settings.json"))

	var config map[string]map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("generated Gemini config is not valid JSON: %v", err)
	}

	entry, ok := config["mcpServers"]["go-boost"]
	if !ok {
		t.Fatalf("Gemini reads the mcpServers key, got: %s", raw)
	}
	if entry["command"] != "go" {
		t.Errorf("command = %v, want go", entry["command"])
	}
}

func TestGeminiConfigKeepsOtherServers(t *testing.T) {
	dir, _ := initProject(t, map[string]string{
		filepath.Join(".gemini", "settings.json"): `{"mcpServers":{"other":{"command":"other-mcp"}},"theme":"dark"}`,
	})

	var config map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".gemini", "settings.json"))), &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if config["theme"] != "dark" {
		t.Error("an unrelated Gemini setting was lost")
	}
	servers := config["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("an existing MCP server entry was removed")
	}
	if _, ok := servers["go-boost"]; !ok {
		t.Error("go-boost was not added")
	}
}
