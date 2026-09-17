package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeCompilerError(t *testing.T) {
	tempDir := t.TempDir()
	srcCode := "package main\n\nfunc main() {\n\tx := undefinedVariable\n\t_ = x\n}\n"
	srcPath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(srcCode), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	rawOutput := srcPath + ":4:7: undefined: undefinedVariable"
	report := AnalyzeErrorOutput(rawOutput, tempDir)
	if report == nil {
		t.Fatalf("expected diagnostic report, got nil")
	}

	if report.Category != "Compilation Error" {
		t.Errorf("expected Category 'Compilation Error', got '%s'", report.Category)
	}
	if report.Line != 4 {
		t.Errorf("expected Line 4, got %d", report.Line)
	}
	if report.Column != 7 {
		t.Errorf("expected Column 7, got %d", report.Column)
	}
	if !strings.Contains(report.Snippet, "->    4 | \tx := undefinedVariable") {
		t.Errorf("expected snippet with target line arrow, got:\n%s", report.Snippet)
	}
}

func TestAnalyzeRuntimePanic(t *testing.T) {
	tempDir := t.TempDir()
	srcCode := "package main\n\ntype Config struct {\n\tPort int\n}\n\nfunc main() {\n\tvar cfg *Config\n\t_ = cfg.Port\n}\n"
	srcPath := filepath.Join(tempDir, "service.go")
	if err := os.WriteFile(srcPath, []byte(srcCode), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	panicOutput := "panic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation]\ngoroutine 1 [running]:\nmain.main()\n\t" + srcPath + ":9 +0x1a\n"

	report := AnalyzeErrorOutput(panicOutput, tempDir)
	if report == nil {
		t.Fatalf("expected diagnostic report, got nil")
	}

	if report.Category != "Runtime Panic" {
		t.Errorf("expected Category 'Runtime Panic', got '%s'", report.Category)
	}
	if !strings.Contains(report.Title, "Nil Pointer Dereference") {
		t.Errorf("expected Title to mention Nil Pointer Dereference, got '%s'", report.Title)
	}
	if report.Line != 9 {
		t.Errorf("expected Line 9, got %d", report.Line)
	}
	if !strings.Contains(report.Snippet, "->    9 | \t_ = cfg.Port") {
		t.Errorf("expected snippet with arrow, got:\n%s", report.Snippet)
	}
}

func TestAnalyzePortConflict(t *testing.T) {
	rawOutput := "2026/09/13 00:00:00 listen tcp :8080: bind: address already in use"
	report := AnalyzeErrorOutput(rawOutput, "")
	if report == nil {
		t.Fatalf("expected diagnostic report, got nil")
	}

	if report.Category != "Network / Port Conflict" {
		t.Errorf("expected Category 'Network / Port Conflict', got '%s'", report.Category)
	}
	if !strings.Contains(report.Title, "8080") {
		t.Errorf("expected port 8080 in title, got '%s'", report.Title)
	}
}

func TestAnalyzeDeadlock(t *testing.T) {
	rawOutput := "fatal error: all goroutines are asleep - deadlock!\n\ngoroutine 1 [chan receive]:\nmain.main()\n\t/app/main.go:10 +0x45\n"
	report := AnalyzeErrorOutput(rawOutput, "")
	if report == nil {
		t.Fatalf("expected diagnostic report, got nil")
	}

	if report.Category != "Concurrency Deadlock" {
		t.Errorf("expected Category 'Concurrency Deadlock', got '%s'", report.Category)
	}
}

func TestRenderBoxAndSave(t *testing.T) {
	tempDir := t.TempDir()
	report := &DiagnosticReport{
		Category:     "Runtime Panic",
		Title:        "Runtime Panic: Nil Pointer Dereference",
		Message:      "invalid memory address or nil pointer dereference",
		File:         "cmd/main.go",
		Line:         42,
		RootCause:    "Dereferenced nil pointer.",
		SuggestedFix: "Check nil before access.",
	}

	box := RenderDiagnosticBox(report)
	if !strings.Contains(box, "GO-BOOST INSTANT DIAGNOSTIC") {
		t.Errorf("expected box header in rendered output")
	}

	if err := SaveReport(report, tempDir); err != nil {
		t.Fatalf("SaveReport failed: %v", err)
	}

	savedFile := filepath.Join(tempDir, ".go-boost", "last_error.json")
	if _, err := os.Stat(savedFile); err != nil {
		t.Errorf("expected saved file at %s", savedFile)
	}
}

func TestAnalyzeCompilerErrorAcceptsWindowsPaths(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantFile string
		wantLine int
	}{
		{
			name:     "unix path",
			output:   "internal/handler/user.go:42:10: undefined: helper",
			wantFile: "internal/handler/user.go",
			wantLine: 42,
		},
		{
			name:     "windows relative path",
			output:   `internal\handler\user.go:42:10: undefined: helper`,
			wantFile: `internal\handler\user.go`,
			wantLine: 42,
		},
		{
			name:     "windows absolute path with drive letter",
			output:   `C:\projects\app\main.go:7:2: imported and not used: "fmt"`,
			wantFile: `C:\projects\app\main.go`,
			wantLine: 7,
		},
		{
			name:     "windows short name with a tilde",
			output:   `C:\Users\RUNNER~1\AppData\Local\Temp\Test123\001\main.go:4:7: undefined: x`,
			wantFile: `C:\Users\RUNNER~1\AppData\Local\Temp\Test123\001\main.go`,
			wantLine: 4,
		},
		{
			name:     "path containing a space",
			output:   `C:\Program Files\app\main.go:12:1: syntax error`,
			wantFile: `C:\Program Files\app\main.go`,
			wantLine: 12,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := AnalyzeErrorOutput(tc.output, "")
			if report == nil {
				t.Fatal("expected a diagnostic report")
			}
			if report.Category != "Compilation Error" {
				t.Errorf("category = %q, want Compilation Error", report.Category)
			}
			if report.File != tc.wantFile {
				t.Errorf("file = %q, want %q", report.File, tc.wantFile)
			}
			if report.Line != tc.wantLine {
				t.Errorf("line = %d, want %d", report.Line, tc.wantLine)
			}
		})
	}
}
