package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateProjectArtifacts(t *testing.T) {
	tempDir := t.TempDir()

	goMod := `module github.com/example/testapp
go 1.24.0
require github.com/gin-gonic/gin v1.9.1
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	res, err := GenerateProjectArtifacts(tempDir, "go-boost")
	if err != nil {
		t.Fatalf("GenerateProjectArtifacts failed: %v", err)
	}

	if len(res.GeneratedFiles) == 0 {
		t.Fatalf("expected generated files, got 0")
	}

	if _, err := os.Stat(filepath.Join(tempDir, ".mcp.json")); err != nil {
		t.Errorf(".mcp.json not created")
	}

	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md not created")
	}

	if _, err := os.Stat(filepath.Join(tempDir, ".cursorrules")); err != nil {
		t.Errorf(".cursorrules not created")
	}

	skillPath := filepath.Join(tempDir, ".ai", "skills", "go-clean-architecture", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("Skill file not created: %s", skillPath)
	}
}
