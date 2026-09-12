package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSuperviseCommandSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := SuperviseCommand(ctx, "", []string{"go", "version"})
	if err != nil {
		t.Fatalf("SuperviseCommand failed: %v", err)
	}
	if report != nil {
		t.Errorf("expected nil diagnostic report for successful command, got %+v", report)
	}
}

func TestSuperviseCommandWithPanic(t *testing.T) {
	tempDir := t.TempDir()

	srcCode := "package main\n\nfunc main() {\n\tpanic(\"simulated test panic\")\n}\n"
	srcFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(srcFile, []byte(srcCode), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := SuperviseCommand(ctx, tempDir, []string{"go", "run", "main.go"})
	if err == nil {
		t.Fatalf("expected command to exit with error")
	}

	if report == nil {
		t.Fatalf("expected diagnostic report for crashing command, got nil")
	}

	if report.Category != "Runtime Panic" {
		t.Errorf("expected Category 'Runtime Panic', got '%s'", report.Category)
	}
	if !strings.Contains(report.Message, "simulated test panic") {
		t.Errorf("expected panic message in report, got '%s'", report.Message)
	}

	savedFile := filepath.Join(tempDir, ".go-boost", "last_error.json")
	if _, err := os.Stat(savedFile); err != nil {
		t.Errorf("expected saved file at %s, error: %v", savedFile, err)
	}
}
