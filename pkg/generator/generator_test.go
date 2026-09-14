package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	for _, name := range []string{".mcp.json", "AGENTS.md", "CLAUDE.md", ".cursorrules", filepath.Join(".cursor", "mcp.json"), filepath.Join(".vscode", "mcp.json")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to be created: %v", name, err)
		}
	}

	if len(res.GeneratedFiles) == 0 {
		t.Error("expected generated files to be reported")
	}
}

func TestInitPreservesExistingUserContent(t *testing.T) {
	handWritten := "# Team rules\n\nAlways rebase before merging.\n"
	dir, _ := initProject(t, map[string]string{
		"CLAUDE.md": handWritten,
		".mcp.json": `{"mcpServers":{"github":{"command":"gh-mcp","args":["serve"]}}}`,
	})

	claude := readFile(t, filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(claude, "Always rebase before merging.") {
		t.Errorf("hand-written CLAUDE.md content was destroyed:\n%s", claude)
	}
	if !strings.Contains(claude, "go-boost") {
		t.Errorf("expected generated guidelines to be appended:\n%s", claude)
	}

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
	before := readFile(t, filepath.Join(dir, "CLAUDE.md"))

	for i := 0; i < 3; i++ {
		if _, err := GenerateProjectArtifacts(dir, "/usr/local/bin/go-boost"); err != nil {
			t.Fatalf("GenerateProjectArtifacts failed: %v", err)
		}
	}

	after := readFile(t, filepath.Join(dir, "CLAUDE.md"))
	if before != after {
		t.Errorf("repeated runs changed the file:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if strings.Count(after, guidelineBeginMarker) != 1 {
		t.Errorf("expected exactly one generated block, got %d", strings.Count(after, guidelineBeginMarker))
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

func TestSkillsAreInstalledInAgentSkillFormat(t *testing.T) {
	dir, res := initProject(t, map[string]string{})

	if len(res.InstalledSkill) == 0 {
		t.Fatal("expected skills to be installed")
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "go-error-handling", "SKILL.md")
	body := readFile(t, skillPath)

	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("SKILL.md must start with YAML frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "name: go-error-handling") {
		t.Errorf("frontmatter is missing the required name field:\n%s", body)
	}
	if !strings.Contains(body, "description: ") {
		t.Errorf("frontmatter is missing the required description field:\n%s", body)
	}
	if strings.Contains(body, `\x60`) {
		t.Errorf("skill body contains escaped backticks instead of real ones:\n%s", body)
	}
	if !strings.Contains(body, "```go") {
		t.Errorf("expected a real fenced code block in the skill body:\n%s", body)
	}
}

func TestCustomSkillsAreInstalledAndOverrideBuiltins(t *testing.T) {
	custom := "---\nname: go-error-handling\ndescription: Our own take.\n---\n\n# House error rules\n"
	dir, _ := initProject(t, map[string]string{
		filepath.Join(".ai", "skills", "go-error-handling", "SKILL.md"): custom,
		filepath.Join(".ai", "skills", "billing-domain", "SKILL.md"):    "---\nname: billing-domain\ndescription: Invoicing rules.\n---\n\n# Billing\n",
	})

	overridden := readFile(t, filepath.Join(dir, ".claude", "skills", "go-error-handling", "SKILL.md"))
	if !strings.Contains(overridden, "House error rules") {
		t.Errorf("a custom skill must override the built-in one, got:\n%s", overridden)
	}

	added := readFile(t, filepath.Join(dir, ".claude", "skills", "billing-domain", "SKILL.md"))
	if !strings.Contains(added, "Billing") {
		t.Errorf("expected the project's own skill to be installed, got:\n%s", added)
	}
}

func TestSkillsAreSelectedByDetectedStack(t *testing.T) {
	flat, _ := initProject(t, map[string]string{})
	if _, err := os.Stat(filepath.Join(flat, ".claude", "skills", "go-clean-architecture", "SKILL.md")); err == nil {
		t.Error("the clean architecture skill should not be installed for a flat project")
	}

	layered, _ := initProject(t, map[string]string{
		filepath.Join("internal", "domain", "user.go"):     "package domain\n",
		filepath.Join("internal", "usecase", "user.go"):    "package usecase\n",
		filepath.Join("internal", "repository", "user.go"): "package repository\n",
	})
	if _, err := os.Stat(filepath.Join(layered, ".claude", "skills", "go-clean-architecture", "SKILL.md")); err != nil {
		t.Errorf("expected the clean architecture skill for a layered project: %v", err)
	}
}

func TestGeneratedGuidelinesListEveryTool(t *testing.T) {
	dir, _ := initProject(t, map[string]string{})
	body := readFile(t, filepath.Join(dir, "AGENTS.md"))

	for _, tool := range ToolCatalog {
		if !strings.Contains(body, "`"+tool.Name+"`") {
			t.Errorf("generated guidelines omit the %q tool", tool.Name)
		}
	}
}

func TestExistingMakefileIsNotOverwritten(t *testing.T) {
	original := ".PHONY: run\n\nrun:\n\t@echo custom\n"
	dir, _ := initProject(t, map[string]string{"Makefile": original})

	if got := readFile(t, filepath.Join(dir, "Makefile")); got != original {
		t.Errorf("an existing Makefile was modified:\n%s", got)
	}
}
