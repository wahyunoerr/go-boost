package detector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetector(t *testing.T) {
	tempDir := t.TempDir()

	goModContent := `module example.com/my-app

go 1.24.0

require (
	github.com/gin-gonic/gin v1.9.1
	gorm.io/gorm v1.25.7
	gorm.io/driver/postgres v1.5.6
	go.uber.org/zap v1.27.0
	github.com/stretchr/testify v1.9.0
)

require (
	github.com/bytedance/sonic v1.9.1 // indirect
)
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	dirs := []string{
		filepath.Join(tempDir, "internal", "domain"),
		filepath.Join(tempDir, "internal", "repository"),
		filepath.Join(tempDir, "internal", "usecase"),
		filepath.Join(tempDir, "internal", "handler"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", d, err)
		}
	}

	detector := NewDetector(tempDir)
	stack, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if stack.ModuleName != "example.com/my-app" {
		t.Errorf("expected module 'example.com/my-app', got '%s'", stack.ModuleName)
	}
	if stack.GoVersion != "1.24.0" {
		t.Errorf("expected Go version '1.24.0', got '%s'", stack.GoVersion)
	}
	if stack.Framework != "gin" {
		t.Errorf("expected framework 'gin', got '%s'", stack.Framework)
	}
	if stack.ORM != "gorm" {
		t.Errorf("expected ORM 'gorm', got '%s'", stack.ORM)
	}
	if stack.DatabaseEngine != "postgres" {
		t.Errorf("expected DatabaseEngine 'postgres', got '%s'", stack.DatabaseEngine)
	}
	if stack.Logger != "zap" {
		t.Errorf("expected Logger 'zap', got '%s'", stack.Logger)
	}
	if stack.Architecture != "clean_architecture" {
		t.Errorf("expected Architecture 'clean_architecture', got '%s'", stack.Architecture)
	}
}

func TestDetectorAdvancedStack(t *testing.T) {
	tempDir := t.TempDir()

	goModContent := `module example.com/microservice

go 1.23.0

require (
	github.com/gorilla/mux v1.8.1
	github.com/redis/go-redis/v9 v9.5.1
	github.com/segmentio/kafka-go v0.4.47
	github.com/spf13/viper v1.18.2
	google.golang.org/grpc v1.62.1
)
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, "sqlc.yaml"), []byte("version: '2'\n"), 0644); err != nil {
		t.Fatalf("failed to write sqlc.yaml: %v", err)
	}

	detector := NewDetector(tempDir)
	stack, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if stack.Framework != "gorilla/mux" {
		t.Errorf("expected Framework 'gorilla/mux', got '%s'", stack.Framework)
	}
	if stack.ORM != "sqlc" {
		t.Errorf("expected ORM 'sqlc', got '%s'", stack.ORM)
	}
	if stack.CacheEngine != "redis" {
		t.Errorf("expected CacheEngine 'redis', got '%s'", stack.CacheEngine)
	}
	if stack.QueueEngine != "kafka" {
		t.Errorf("expected QueueEngine 'kafka', got '%s'", stack.QueueEngine)
	}
	if stack.ConfigManager != "viper" {
		t.Errorf("expected ConfigManager 'viper', got '%s'", stack.ConfigManager)
	}
	if stack.RPC != "grpc" {
		t.Errorf("expected RPC 'grpc', got '%s'", stack.RPC)
	}
}
