package astparser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckConcurrency(t *testing.T) {
	tempDir := t.TempDir()

	src := `package worker

import (
	"context"
	"sync"
	"time"
)

func LeakyContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = ctx
}

func LeakyMutex() {
	var mu sync.Mutex
	mu.Lock()
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "worker.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write worker.go: %v", err)
	}

	issues, err := CheckConcurrency(tempDir)
	if err != nil {
		t.Fatalf("CheckConcurrency failed: %v", err)
	}

	if len(issues) < 2 {
		t.Fatalf("expected at least 2 concurrency issues, got %d", len(issues))
	}

	var hasContextLeak, hasMutexLeak bool
	for _, iss := range issues {
		if iss.Type == "context_leak" {
			hasContextLeak = true
		}
		if iss.Type == "mutex_leak" {
			hasMutexLeak = true
		}
	}

	if !hasContextLeak {
		t.Errorf("expected context leak to be detected")
	}
	if !hasMutexLeak {
		t.Errorf("expected mutex leak to be detected")
	}
}
