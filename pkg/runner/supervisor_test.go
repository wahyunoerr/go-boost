package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSuperviseCommandSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := SuperviseCommand(ctx, "", []string{"go", "version"}, SuperviseOptions{})
	if err != nil {
		t.Fatalf("SuperviseCommand failed: %v", err)
	}
	if res.Report != nil {
		t.Errorf("expected nil diagnostic report for successful command, got %+v", res.Report)
	}
	if !res.Succeeded {
		t.Errorf("expected the command to be reported as succeeded, got exit code %d", res.ExitCode)
	}
}

func TestSuperviseCommandWithPanic(t *testing.T) {
	tempDir := t.TempDir()

	srcCode := "package main\n\nfunc main() {\n\tpanic(\"simulated test panic\")\n}\n"
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(srcCode), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := SuperviseCommand(ctx, tempDir, []string{"go", "run", "main.go"}, SuperviseOptions{})
	if err == nil {
		t.Fatal("expected command to exit with error")
	}
	if res == nil || res.Report == nil {
		t.Fatal("expected a diagnostic report for a crashing command")
	}
	if res.Report.Category != "Runtime Panic" {
		t.Errorf("expected Category 'Runtime Panic', got %q", res.Report.Category)
	}
	if !strings.Contains(res.Report.Message, "simulated test panic") {
		t.Errorf("expected panic message in report, got %q", res.Report.Message)
	}
	if res.Succeeded {
		t.Error("expected Succeeded to be false for a panicking command")
	}

	savedFile := filepath.Join(tempDir, ".go-boost", "last_error.json")
	if _, err := os.Stat(savedFile); err != nil {
		t.Errorf("expected saved report at %s, error: %v", savedFile, err)
	}
}

func TestSuperviseCommandDoesNotWriteToProcessStdout(t *testing.T) {
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := SuperviseCommand(ctx, "", []string{"echo", "THIS-MUST-NOT-REACH-STDOUT"}, SuperviseOptions{
		Stdout: &out,
		Stderr: &out,
	})
	if err != nil {
		t.Fatalf("SuperviseCommand failed: %v", err)
	}
	if !strings.Contains(out.String(), "THIS-MUST-NOT-REACH-STDOUT") {
		t.Errorf("expected the child's output to be routed to the supplied writer, got %q", out.String())
	}
}

func TestSuperviseCommandHandlesVeryLongLines(t *testing.T) {
	tempDir := t.TempDir()
	srcCode := `package main

import (
	"os"
	"strings"
)

func main() {
	os.Stdout.WriteString(strings.Repeat("a", 300000) + "\n")
	os.Stdout.WriteString(strings.Repeat("b", 300000) + "\n")
	os.Stdout.WriteString("DONE\n")
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(srcCode), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var out bytes.Buffer
	res, err := SuperviseCommand(ctx, tempDir, []string{"go", "run", "main.go"}, SuperviseOptions{
		Stdout:  &out,
		Stderr:  &out,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("SuperviseCommand failed on long output: %v", err)
	}
	if res.TimedOut {
		t.Fatal("command deadlocked on a long line instead of completing")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Error("expected the reader to keep draining the pipe past a very long line")
	}
}

func TestSuperviseCommandTimesOutOnLongRunningCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	res, err := SuperviseCommand(ctx, "", []string{"sleep", "30"}, SuperviseOptions{
		Timeout: 2 * time.Second,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if res == nil || !res.TimedOut {
		t.Fatal("expected the result to be marked as timed out")
	}
	if elapsed > 20*time.Second {
		t.Errorf("timeout took %s; the command was not stopped promptly", elapsed)
	}
}
