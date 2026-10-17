package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wahyunoerr/go-boost/v2/pkg/detector"
)

type InitResult struct {
	GeneratedFiles []string `json:"generated_files"`
	UpdatedFiles   []string `json:"updated_files"`
	SkippedFiles   []string `json:"skipped_files"`
	InstalledSkill []string `json:"installed_skills"`
	PortableConfig bool     `json:"portable_config"`
}

const serverKey = "go-boost"

func GenerateProjectArtifacts(rootDir string, binaryPath string) (*InitResult, error) {
	d := detector.NewDetector(rootDir)
	stack, err := d.Detect()
	if err != nil {
		return nil, fmt.Errorf("failed to detect project stack: %w", err)
	}

	result := &InitResult{
		GeneratedFiles: make([]string, 0),
		UpdatedFiles:   make([]string, 0),
		SkippedFiles:   make([]string, 0),
		InstalledSkill: make([]string, 0),
	}

	invocation := ResolveMCPInvocation(rootDir, binaryPath)
	result.PortableConfig = invocation.Portable

	if err := mergeMCPConfig(filepath.Join(rootDir, ".mcp.json"), "mcpServers", invocation, result); err != nil {
		return nil, err
	}
	if err := mergeMCPConfig(filepath.Join(rootDir, ".cursor", "mcp.json"), "mcpServers", invocation, result); err != nil {
		return nil, err
	}
	if err := mergeMCPConfig(filepath.Join(rootDir, ".vscode", "mcp.json"), "servers", invocation, result); err != nil {
		return nil, err
	}
	if err := mergeMCPConfig(filepath.Join(rootDir, ".gemini", "settings.json"), "mcpServers", invocation, result); err != nil {
		return nil, err
	}

	if err := installSkills(rootDir, stack, result); err != nil {
		return nil, err
	}

	if err := ensureGitignored(rootDir, ".go-boost/", result); err != nil {
		return nil, err
	}

	return result, nil
}

func mergeMCPConfig(path, key string, invocation MCPInvocation, result *InitResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	config := map[string]any{}
	existing, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(existing, &config); err != nil {
			result.SkippedFiles = append(result.SkippedFiles, path+" (not valid JSON; left untouched)")
			return nil
		}
	}

	servers, _ := config[key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[serverKey] = invocation.entry()
	config[key] = servers

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if string(existing) == string(data) {
		return nil
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	record(result, path, len(existing) > 0)
	return nil
}

func installSkills(rootDir string, stack *detector.ProjectStack, result *InitResult) error {
	skills := map[string]string{}
	order := []string{}

	for _, skill := range SkillsFor(stack) {
		skills[skill.Name] = skill.Render()
		order = append(order, skill.Name)
	}

	custom, err := loadCustomSkills(rootDir)
	if err != nil {
		return err
	}
	for name, body := range custom {
		if _, exists := skills[name]; !exists {
			order = append(order, name)
		}
		skills[name] = body
	}
	sort.Strings(order)

	for _, name := range order {
		target := filepath.Join(rootDir, ".claude", "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := writeFileIfChanged(target, skills[name], result); err != nil {
			return err
		}
		result.InstalledSkill = append(result.InstalledSkill, name)
	}

	return nil
}

func loadCustomSkills(rootDir string) (map[string]string, error) {
	skillsDir := filepath.Join(rootDir, ".ai", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil
	}

	custom := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(skillsDir, entry.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		custom[entry.Name()] = string(body)
	}

	return custom, nil
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

	record(result, filePath, err == nil)
	return nil
}

func record(result *InitResult, path string, updated bool) {
	if updated {
		result.UpdatedFiles = append(result.UpdatedFiles, path)
		return
	}
	result.GeneratedFiles = append(result.GeneratedFiles, path)
}

func ensureGitignored(rootDir, pattern string, result *InitResult) error {
	gitignorePath := filepath.Join(rootDir, ".gitignore")

	existing, err := os.ReadFile(gitignorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == strings.TrimSuffix(pattern, "/") || strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	content := strings.TrimRight(string(existing), "\n") + "\n\n" + pattern + "\n"
	if err := os.WriteFile(gitignorePath, []byte(content), 0644); err != nil {
		return err
	}

	result.UpdatedFiles = append(result.UpdatedFiles, gitignorePath)
	return nil
}
