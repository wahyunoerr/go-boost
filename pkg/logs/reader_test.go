package logs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogReaderAndLastError(t *testing.T) {
	tempDir := t.TempDir()

	logContent := `{"time":"2026-09-12T10:00:00Z","level":"INFO","msg":"server started","caller":"main.go:25"}
{"time":"2026-09-12T10:01:00Z","level":"WARN","msg":"high latency detected"}
2026/09/12 10:02:00 [ERROR] failed to connect to redis: connection refused
goroutine 42 [running]:
main.connectRedis()
	/app/redis.go:45 +0x3a
`
	logPath := filepath.Join(tempDir, "app.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to write app.log: %v", err)
	}

	entries, err := ReadEntries(tempDir, 10)
	if err != nil {
		t.Fatalf("ReadEntries failed: %v", err)
	}

	if len(entries) < 3 {
		t.Fatalf("expected at least 3 log entries, got %d", len(entries))
	}

	lastErr, err := FindLastError(tempDir)
	if err != nil {
		t.Fatalf("FindLastError failed: %v", err)
	}

	if lastErr.Type != "error" {
		t.Errorf("expected type 'error', got '%s'", lastErr.Type)
	}
	if lastErr.Message == "" {
		t.Errorf("expected error message, got empty")
	}
}
