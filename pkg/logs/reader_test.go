package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}
	return dir
}

func TestReadEntriesParsesJSONLogs(t *testing.T) {
	dir := writeLog(t, `{"time":"2026-09-13T10:00:00Z","level":"INFO","msg":"server started"}
{"time":"2026-09-13T10:00:01Z","level":"ERROR","msg":"database unreachable","caller":"db.go:42"}
`)

	entries, err := ReadEntries(dir, 50)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[1].Level != "ERROR" || entries[1].Message != "database unreachable" {
		t.Errorf("unexpected entry: %+v", entries[1])
	}
	if entries[1].Caller != "db.go:42" {
		t.Errorf("caller = %q, want db.go:42", entries[1].Caller)
	}
}

func TestReadEntriesParsesSlogTextHandlerOutput(t *testing.T) {
	dir := writeLog(t, `time=2026-09-13T10:00:00.000Z level=INFO msg=started
time=2026-09-13T10:00:01.000Z level=ERROR msg="db down" attempt=3
time=2026-09-13T10:00:02.000Z level=INFO msg=retry
`)

	entries, err := ReadEntries(dir, 50)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}

	if entries[1].Level != "ERROR" {
		t.Errorf("level = %q, want ERROR", entries[1].Level)
	}
	if entries[1].Message != "db down" {
		t.Errorf("message = %q, want the quoted value to be unwrapped", entries[1].Message)
	}
	if entries[0].Timestamp == "" {
		t.Error("expected a timestamp to be extracted")
	}
}

func TestFindLastErrorFindsSlogTextError(t *testing.T) {
	dir := writeLog(t, `time=2026-09-13T10:00:00Z level=INFO msg=started
time=2026-09-13T10:00:01Z level=ERROR msg="payment failed" order=42
`)

	info, err := FindLastError(dir)
	if err != nil {
		t.Fatalf("FindLastError failed: %v", err)
	}
	if info.Type == "none" {
		t.Fatalf("expected an error to be found, got %+v", info)
	}
	if !strings.Contains(info.Message, "payment failed") {
		t.Errorf("message = %q, want it to mention the failure", info.Message)
	}
}

func TestReadEntriesKeepsStackTracesAttached(t *testing.T) {
	dir := writeLog(t, `2026/09/13 10:00:00 request failed
panic: runtime error: invalid memory address
goroutine 1 [running]:
main.handler(0x0)
	/app/handler.go:17 +0x1d
`)

	entries, err := ReadEntries(dir, 50)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the trace to stay with its entry, got %d entries", len(entries))
	}
	if !strings.Contains(entries[0].Stack, "handler.go:17") {
		t.Errorf("expected the stack trace to be captured, got %q", entries[0].Stack)
	}

	info, err := FindLastError(dir)
	if err != nil {
		t.Fatalf("FindLastError failed: %v", err)
	}
	if info.Type != "panic" {
		t.Errorf("type = %q, want panic", info.Type)
	}
	if info.File != "/app/handler.go" || info.Line != "17" {
		t.Errorf("location = %s:%s, want /app/handler.go:17", info.File, info.Line)
	}
}

func TestReadEntriesReadsZapStacktraceField(t *testing.T) {
	dir := writeLog(t, `{"level":"error","ts":"2026-09-13T10:00:00Z","msg":"boom","stacktrace":"main.go:10"}
`)

	entries, err := ReadEntries(dir, 10)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Stack != "main.go:10" {
		t.Errorf("expected the zap stacktrace field to be read, got %+v", entries)
	}
}

func TestReadEntriesSurvivesVeryLongLines(t *testing.T) {
	huge := strings.Repeat("x", 200000)
	dir := writeLog(t, `{"level":"info","msg":"`+huge+`"}
{"level":"ERROR","msg":"the newest error"}
`)

	entries, err := ReadEntries(dir, 50)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries to be returned")
	}
	last := entries[len(entries)-1]
	if last.Message != "the newest error" {
		t.Errorf("expected the entry after the long line to be read, got %q", last.Message)
	}
}

func TestFindLastErrorPrefersTheNewestSource(t *testing.T) {
	dir := t.TempDir()

	boostDir := filepath.Join(dir, ".go-boost")
	if err := os.MkdirAll(boostDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	saved := map[string]any{
		"category":  "Runtime Panic",
		"message":   "an old crash from last week",
		"timestamp": time.Now().Add(-72 * time.Hour).Format(time.RFC3339),
	}
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(filepath.Join(boostDir, "last_error.json"), data, 0o644); err != nil {
		t.Fatalf("write saved report: %v", err)
	}

	recent := time.Now().Add(-1 * time.Minute).Format(time.RFC3339)
	logLine := `{"time":"` + recent + `","level":"ERROR","msg":"a much newer failure"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte(logLine), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	info, err := FindLastError(dir)
	if err != nil {
		t.Fatalf("FindLastError failed: %v", err)
	}
	if !strings.Contains(info.Message, "a much newer failure") {
		t.Errorf("expected the newer log error to win, got %q from %q", info.Message, info.Source)
	}
}

func TestFindLastErrorReportsNothingWhenClean(t *testing.T) {
	dir := writeLog(t, `{"level":"INFO","msg":"all good"}
`)

	info, err := FindLastError(dir)
	if err != nil {
		t.Fatalf("FindLastError failed: %v", err)
	}
	if info.Type != "none" {
		t.Errorf("expected no error to be reported, got %+v", info)
	}
}
