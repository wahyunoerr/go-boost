package generator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wahyunoerr/go-boost/pkg/detector"
)

type InitResult struct {
	GeneratedFiles []string `json:"generated_files"`
}

func GenerateProjectArtifacts(rootDir string, binaryPath string) (*InitResult, error) {
	d := detector.NewDetector(rootDir)
	stack, err := d.Detect()
	if err != nil {
		return nil, fmt.Errorf("failed to detect project stack: %w", err)
	}

	result := &InitResult{
		GeneratedFiles: make([]string, 0),
	}

	mcpContent := GenerateMCPConfigJSON(binaryPath)
	if err := writeFileIfChanged(filepath.Join(rootDir, ".mcp.json"), mcpContent, result); err != nil {
		return nil, err
	}

	cursorDir := filepath.Join(rootDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err == nil {
		_ = writeFileIfChanged(filepath.Join(cursorDir, "mcp.json"), mcpContent, result)
	}

	vscodeDir := filepath.Join(rootDir, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0755); err == nil {
		_ = writeFileIfChanged(filepath.Join(vscodeDir, "mcp.json"), mcpContent, result)
	}

	agentsMD := GenerateAgentsMD(stack)
	if err := writeFileIfChanged(filepath.Join(rootDir, "AGENTS.md"), agentsMD, result); err != nil {
		return nil, err
	}

	claudeMD := GenerateClaudeMD(stack)
	if err := writeFileIfChanged(filepath.Join(rootDir, "CLAUDE.md"), claudeMD, result); err != nil {
		return nil, err
	}

	cursorRules := GenerateCursorRules(stack)
	if err := writeFileIfChanged(filepath.Join(rootDir, ".cursorrules"), cursorRules, result); err != nil {
		return nil, err
	}

	for skillName, skillContent := range BuiltinSkills {
		skillDir := filepath.Join(rootDir, ".ai", "skills", skillName)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			continue
		}
		skillFile := filepath.Join(skillDir, "SKILL.md")
		_ = writeFileIfChanged(skillFile, skillContent, result)
	}

	makefilePath := filepath.Join(rootDir, "Makefile")
	if _, err := os.Stat(makefilePath); os.IsNotExist(err) {
		_ = writeFileIfChanged(makefilePath, GenerateMakefile(), result)
	}

	return result, nil
}

func writeFileIfChanged(filePath string, content string, result *InitResult) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	existing, err := os.ReadFile(filePath)
	if err == nil && string(existing) == content {
		return nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}

	result.GeneratedFiles = append(result.GeneratedFiles, filePath)
	return nil
}
