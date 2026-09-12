package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateOpenAPISpec(t *testing.T) {
	tempDir := t.TempDir()

	src := `package router

type Engine interface {
	GET(path string, handler any)
	POST(path string, handler any)
}

func Register(r Engine) {
	r.GET("/api/v1/users/:id", GetUser)
	r.POST("/api/v1/users", CreateUser)
}

func GetUser() {}
func CreateUser() {}
`
	if err := os.WriteFile(filepath.Join(tempDir, "routes.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write routes.go: %v", err)
	}

	spec, err := GenerateOpenAPISpec(tempDir, "Test API")
	if err != nil {
		t.Fatalf("GenerateOpenAPISpec failed: %v", err)
	}

	if !strings.Contains(spec, `"openapi": "3.0.3"`) {
		t.Errorf("expected openapi 3.0.3 version in spec")
	}
	if !strings.Contains(spec, `"/api/v1/users/{id}"`) {
		t.Errorf("expected normalized path /api/v1/users/{id} in spec")
	}
	if !strings.Contains(spec, `"title": "Test API"`) {
		t.Errorf("expected title in spec")
	}
}
