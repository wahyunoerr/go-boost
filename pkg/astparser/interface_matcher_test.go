package astparser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindImplementations(t *testing.T) {
	tempDir := t.TempDir()

	src := `package service

type Greeter interface {
	Greet(name string) string
}

type EnglishGreeter struct{}

func (e *EnglishGreeter) Greet(name string) string {
	return "Hello " + name
}

type PartialGreeter struct{}

func (p *PartialGreeter) Wave() {}
`
	if err := os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write service.go: %v", err)
	}

	matches, err := FindImplementations(tempDir, "Greeter")
	if err != nil {
		t.Fatalf("FindImplementations failed: %v", err)
	}

	if len(matches) == 0 {
		t.Fatalf("expected at least 1 match, got 0")
	}

	var foundEnglish bool
	for _, m := range matches {
		if m.StructName == "EnglishGreeter" {
			foundEnglish = true
			if !m.IsComplete {
				t.Errorf("expected EnglishGreeter to completely implement Greeter")
			}
			if len(m.MatchedMethods) != 1 || m.MatchedMethods[0] != "Greet" {
				t.Errorf("expected Greet to match, got %v", m.MatchedMethods)
			}
		}
	}

	if !foundEnglish {
		t.Errorf("expected EnglishGreeter to be found as an implementation")
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestFindImplementationsIsPackageAware(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"users/repo.go": `package users

import "context"

type Repository interface {
	Find(ctx context.Context, id string) (string, error)
}

type PostgresRepo struct{}

func (r *PostgresRepo) Find(ctx context.Context, id string) (string, error) { return "", nil }
`,
		"orders/repo.go": `package orders

type PostgresRepo struct{}

func (r *PostgresRepo) Total() int { return 0 }
`,
	})

	matches, err := FindImplementations(dir, "Repository")
	if err != nil {
		t.Fatalf("FindImplementations failed: %v", err)
	}

	for _, m := range matches {
		if m.Package == "orders" && m.IsComplete {
			t.Errorf("a same-named struct in another package was reported as an implementation: %+v", m)
		}
	}

	var found bool
	for _, m := range matches {
		if m.Package == "users" && m.StructName == "PostgresRepo" && m.IsComplete {
			found = true
		}
	}
	if !found {
		t.Errorf("expected users.PostgresRepo to implement Repository, got %+v", matches)
	}
}

func TestFindImplementationsResolvesEmbeddedInterfaces(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"store.go": `package store

type Reader interface {
	Get(id string) (string, error)
}

type Writer interface {
	Put(id string, value string) error
}

type Store interface {
	Reader
	Writer
}

type Memory struct{}

func (m *Memory) Get(id string) (string, error)      { return "", nil }
func (m *Memory) Put(id string, value string) error  { return nil }

type Partial struct{}

func (p *Partial) Get(id string) (string, error) { return "", nil }
`,
	})

	matches, err := FindImplementations(dir, "Store")
	if err != nil {
		t.Fatalf("FindImplementations failed: %v", err)
	}

	byName := map[string]ImplementationMatch{}
	for _, m := range matches {
		byName[m.StructName] = m
	}

	if !byName["Memory"].IsComplete {
		t.Errorf("expected Memory to satisfy the embedded method set, got %+v", byName["Memory"])
	}
	if byName["Partial"].IsComplete {
		t.Errorf("expected Partial to be incomplete, got %+v", byName["Partial"])
	}
	if len(byName["Partial"].MissingMethods) != 1 || byName["Partial"].MissingMethods[0] != "Put" {
		t.Errorf("expected Put to be reported missing, got %+v", byName["Partial"].MissingMethods)
	}
}

func TestDetectDeadCodeIsPackageAware(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a/a.go": `package a

func Helper() int { return 1 }

func Used() int { return Helper() }
`,
		"b/b.go": `package b

func Helper() int { return 2 }
`,
		"a/use.go": `package a

func Caller() int { return Used() }
`,
	})

	unused, err := DetectDeadCode(dir)
	if err != nil {
		t.Fatalf("DetectDeadCode failed: %v", err)
	}

	names := map[string]string{}
	for _, u := range unused {
		names[u.Package+"."+u.Name] = u.File
	}

	if _, ok := names["a.Helper"]; ok {
		t.Errorf("a.Helper is called by a.Used and must not be reported: %+v", unused)
	}
	if _, ok := names["b.Helper"]; !ok {
		t.Errorf("b.Helper is never referenced and should be reported: %+v", unused)
	}
}

func TestDetectDeadCodeIgnoresFieldNameCollisions(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.go": `package main

type Config struct{}

type Options struct {
	Config string
}

func main() {
	_ = Options{Config: "x"}
}
`,
	})

	unused, err := DetectDeadCode(dir)
	if err != nil {
		t.Fatalf("DetectDeadCode failed: %v", err)
	}

	var found bool
	for _, u := range unused {
		if u.Name == "Config" && u.Kind == "struct" {
			found = true
		}
	}
	if !found {
		t.Errorf("a struct used only as a field name must still be reported as dead: %+v", unused)
	}
}
