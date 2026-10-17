package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupDoc(t *testing.T) {
	doc, err := LookupDoc(context.Background(), ".", "fmt.Println")
	if err != nil {
		t.Fatalf("LookupDoc failed: %v", err)
	}
	if doc == "" {
		t.Error("expected doc for fmt.Println, got empty")
	}
}

func TestCodeCheck(t *testing.T) {
	res, err := CodeCheck(context.Background(), "..", "./mcp/...")
	if err != nil {
		t.Fatalf("CodeCheck failed: %v", err)
	}
	if res == "" {
		t.Error("expected non-empty check result")
	}
}

func TestGoToolArgumentsCannotBecomeFlags(t *testing.T) {
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "SHOULD_NOT_EXIST")
	tool := filepath.Join(t.TempDir(), "tool.sh")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatalf("failed to write helper: %v", err)
	}

	t.Run("code_check", func(t *testing.T) {
		if _, err := CodeCheck(ctx, "..", "-vettool="+tool); err == nil {
			t.Error("expected -vettool to be rejected")
		}
	})

	t.Run("test_runner", func(t *testing.T) {
		if _, err := RunTests(ctx, "..", TestOptions{Package: "-exec=" + tool}); err == nil {
			t.Error("expected -exec to be rejected")
		}
	})

	t.Run("bench_runner", func(t *testing.T) {
		if _, err := RunBenchmark(ctx, "..", "-exec="+tool, "."); err == nil {
			t.Error("expected -exec to be rejected")
		}
	})

	t.Run("go_doc", func(t *testing.T) {
		if _, err := LookupDoc(ctx, "..", "-all"); err == nil {
			t.Error("expected a flag-shaped symbol to be rejected")
		}
	})

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a go tool executed the helper binary; argument validation failed")
	}
}

func writeModule(t *testing.T, files map[string]string) string {
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

func TestRunTestsReportsFailureDetails(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module failing\n\ngo 1.22\n",
		"calc_test.go": `package failing

import "testing"

func TestSum(t *testing.T) {
	t.Errorf("sum mismatch: got 3, want 2")
}

func TestPasses(t *testing.T) {}
`,
	})

	res, err := RunTests(context.Background(), dir, TestOptions{})
	if err != nil {
		t.Fatalf("RunTests failed: %v", err)
	}
	if res.Success {
		t.Fatal("expected the run to be reported as failed")
	}
	if len(res.FailedTests) != 1 {
		t.Fatalf("expected exactly 1 failed test, got %d: %+v", len(res.FailedTests), res.FailedTests)
	}

	ft := res.FailedTests[0]
	if ft.TestName != "TestSum" {
		t.Errorf("test_name = %q, want TestSum", ft.TestName)
	}
	if ft.Package != "failing" {
		t.Errorf("package = %q, want failing", ft.Package)
	}
	if !strings.Contains(ft.Message, "sum mismatch: got 3, want 2") {
		t.Errorf("message = %q, want it to contain the assertion text", ft.Message)
	}
	if ft.File != "calc_test.go" {
		t.Errorf("file = %q, want calc_test.go", ft.File)
	}
	if ft.Line == "" {
		t.Error("expected a line number for the failure")
	}
}

func TestRunTestsReportsBuildErrors(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":         "module broken\n\ngo 1.22\n",
		"broken.go":      "package broken\n\nfunc Broken() int { return undefinedSymbol }\n",
		"broken_test.go": "package broken\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { _ = Broken() }\n",
	})

	res, err := RunTests(context.Background(), dir, TestOptions{})
	if err != nil {
		t.Fatalf("RunTests failed: %v", err)
	}
	if res.Success {
		t.Fatal("expected a build failure to be reported as failure")
	}
	if len(res.BuildErrors) == 0 {
		t.Fatalf("expected build errors to be surfaced, got summary %q and raw output:\n%s", res.Summary, res.RawOutput)
	}
	joined := strings.Join(res.BuildErrors, "\n")
	if !strings.Contains(joined, "undefinedSymbol") {
		t.Errorf("build errors = %q, want them to name the undefined symbol", joined)
	}
	if !strings.Contains(res.Summary, "Build failed") {
		t.Errorf("summary = %q, want it to say the build failed", res.Summary)
	}
}

func TestRunTestsSucceeds(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":     "module passing\n\ngo 1.22\n",
		"ok_test.go": "package passing\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n",
	})

	res, err := RunTests(context.Background(), dir, TestOptions{Coverage: true})
	if err != nil {
		t.Fatalf("RunTests failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got summary %q with output:\n%s", res.Summary, res.RawOutput)
	}
	if len(res.FailedTests) != 0 {
		t.Errorf("expected no failed tests, got %+v", res.FailedTests)
	}
	if !strings.Contains(res.RawOutput, "PASS") && !strings.Contains(res.RawOutput, "ok") {
		t.Errorf("expected readable test output, got: %q", res.RawOutput)
	}
}

func TestTruncateTestOutputKeepsTheTailWhenThereAreFailures(t *testing.T) {
	body := strings.Repeat("noise line that is not interesting\n", 2000)
	raw := body + "FINAL FAILURE MARKER\n"

	got := truncateTestOutput(raw, []FailedTest{{TestName: "TestX"}}, nil)
	if !strings.Contains(got, "FINAL FAILURE MARKER") {
		t.Error("expected the end of the log to be preserved when tests failed")
	}
	if len(got) > maxOutputBytes+200 {
		t.Errorf("truncated output is %d bytes, want roughly %d", len(got), maxOutputBytes)
	}
}
