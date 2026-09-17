package detector

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProjectLayout struct {
	Style        string              `json:"style"`
	MainPackages []string            `json:"main_packages,omitempty"`
	SourceRoots  []string            `json:"source_roots,omitempty"`
	Layers       map[string][]string `json:"layers,omitempty"`
	Features     []string            `json:"features,omitempty"`
	Migrations   []string            `json:"migrations,omitempty"`
	ConfigDirs   []string            `json:"config_dirs,omitempty"`
	TestStyle    string              `json:"test_style,omitempty"`
}

var layerAliases = map[string][]string{
	"handler":    {"handler", "handlers", "delivery", "transport", "controller", "controllers", "api", "rest", "http", "endpoint", "endpoints"},
	"usecase":    {"usecase", "usecases", "service", "services", "application", "app", "business", "logic", "interactor"},
	"repository": {"repository", "repositories", "repo", "repos", "store", "storage", "persistence", "dao", "gateway"},
	"domain":     {"domain", "entity", "entities", "model", "models", "core"},
	"transport":  {"grpc", "graphql", "websocket", "consumer", "worker", "workers", "job", "jobs", "cron", "scheduler"},
	"infra":      {"infra", "infrastructure", "platform", "adapter", "adapters", "provider", "providers", "client", "clients"},
	"config":     {"config", "configs", "configuration", "settings"},
	"middleware": {"middleware", "middlewares", "interceptor", "interceptors"},
}

var sourceRootNames = map[string]bool{
	"internal": true, "pkg": true, "app": true, "src": true, "lib": true, "modules": true, "services": true,
}

var migrationDirNames = map[string]bool{
	"migration": true, "migrations": true, "migrate": true, "schema": true, "db": true,
}

func DetectLayout(rootDir string) *ProjectLayout {
	layout := &ProjectLayout{
		Style:  "flat",
		Layers: map[string][]string{},
	}

	dirs := collectProjectDirs(rootDir)

	layerOwners := map[string]map[string]bool{}
	featureSet := map[string]bool{}
	sourceRootSet := map[string]bool{}

	for _, rel := range dirs {
		segments := strings.Split(rel, "/")
		base := strings.ToLower(segments[len(segments)-1])

		if len(segments) == 1 && sourceRootNames[base] {
			sourceRootSet[rel] = true
		}

		if migrationDirNames[base] && dirHasFiles(filepath.Join(rootDir, filepath.FromSlash(rel)), ".sql", ".go") {
			layout.Migrations = append(layout.Migrations, rel)
		}

		if isEntryPointDir(segments) {
			continue
		}

		layer, ok := classifyLayer(base)
		if !ok {
			continue
		}
		if layer == "config" {
			layout.ConfigDirs = append(layout.ConfigDirs, rel)
			continue
		}
		if !dirHasFiles(filepath.Join(rootDir, filepath.FromSlash(rel)), ".go") {
			continue
		}

		if layerOwners[layer] == nil {
			layerOwners[layer] = map[string]bool{}
		}
		layerOwners[layer][rel] = true

		if feature := featureNameFor(segments); feature != "" {
			featureSet[feature] = true
		}
	}

	for layer, paths := range layerOwners {
		list := make([]string, 0, len(paths))
		for p := range paths {
			list = append(list, p)
		}
		sort.Strings(list)
		layout.Layers[layer] = list
	}

	layout.SourceRoots = sortedKeys(sourceRootSet)
	layout.Features = sortedKeys(featureSet)
	layout.MainPackages = detectMainPackages(rootDir, dirs)
	layout.Migrations = dedupe(layout.Migrations)
	layout.ConfigDirs = dedupe(layout.ConfigDirs)
	layout.Style = classifyStyle(layout)
	layout.TestStyle = detectTestStyle(rootDir, dirs)

	return layout
}

func isEntryPointDir(segments []string) bool {
	return len(segments) >= 2 && strings.EqualFold(segments[0], "cmd")
}

func classifyLayer(base string) (string, bool) {
	for layer, aliases := range layerAliases {
		for _, alias := range aliases {
			if base == alias {
				return layer, true
			}
		}
	}
	return "", false
}

func featureNameFor(segments []string) string {
	if len(segments) < 3 {
		return ""
	}
	parent := segments[len(segments)-2]
	grandparent := segments[len(segments)-3]

	if !sourceRootNames[strings.ToLower(grandparent)] {
		return ""
	}
	if _, isLayer := classifyLayer(strings.ToLower(parent)); isLayer {
		return ""
	}
	return parent
}

func classifyStyle(layout *ProjectLayout) string {
	if len(layout.Features) >= 2 {
		return "feature"
	}
	if len(layout.Layers) >= 3 {
		return "layered"
	}
	if len(layout.Layers) > 0 {
		return "partial"
	}
	return "flat"
}

func detectMainPackages(rootDir string, dirs []string) []string {
	var mains []string

	if hasMainPackage(rootDir) {
		mains = append(mains, ".")
	}
	for _, rel := range dirs {
		if hasMainPackage(filepath.Join(rootDir, filepath.FromSlash(rel))) {
			mains = append(mains, "./"+rel)
		}
	}

	sort.Strings(mains)
	return mains
}

func hasMainPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if declaresMainPackage(string(data)) {
			return true
		}
	}
	return false
}

func declaresMainPackage(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "package ")) == "main"
		}
	}
	return false
}

func detectTestStyle(rootDir string, dirs []string) string {
	external := 0
	internal := 0

	check := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "package ") {
					continue
				}
				if strings.HasSuffix(line, "_test") {
					external++
				} else {
					internal++
				}
				break
			}
		}
	}

	check(rootDir)
	for _, rel := range dirs {
		check(filepath.Join(rootDir, filepath.FromSlash(rel)))
	}

	switch {
	case internal == 0 && external == 0:
		return ""
	case external > internal:
		return "external (package foo_test)"
	default:
		return "internal (package foo)"
	}
}

func collectProjectDirs(rootDir string) []string {
	var dirs []string

	_ = filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil || !fi.IsDir() {
			return nil
		}
		if path == rootDir {
			return nil
		}
		name := fi.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
			name == "vendor" || name == "node_modules" || name == "testdata" ||
			name == "third_party" || name == "bin" || name == "dist" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		if strings.Count(rel, string(filepath.Separator)) > 4 {
			return filepath.SkipDir
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})

	sort.Strings(dirs)
	return dirs
}

func dirHasFiles(dir string, extensions ...string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		for _, ext := range extensions {
			if strings.HasSuffix(e.Name(), ext) {
				return true
			}
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
