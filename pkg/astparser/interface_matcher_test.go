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
