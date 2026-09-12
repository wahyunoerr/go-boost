package astparser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectDeadCode(t *testing.T) {
	tempDir := t.TempDir()

	src := `package service

func UsedHelper() string {
	return "used"
}

func DeadHelper() string {
	return "dead"
}

func Caller() {
	_ = UsedHelper()
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write service.go: %v", err)
	}

	unused, err := DetectDeadCode(tempDir)
	if err != nil {
		t.Fatalf("DetectDeadCode failed: %v", err)
	}

	var foundDeadHelper bool
	var foundUsedHelper bool

	for _, u := range unused {
		if u.Name == "DeadHelper" {
			foundDeadHelper = true
		}
		if u.Name == "UsedHelper" {
			foundUsedHelper = true
		}
	}

	if !foundDeadHelper {
		t.Errorf("expected DeadHelper to be identified as unused")
	}
	if foundUsedHelper {
		t.Errorf("expected UsedHelper to NOT be identified as unused")
	}
}
