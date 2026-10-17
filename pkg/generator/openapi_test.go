package generator

import (
	"encoding/json"
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

	spec, err := GenerateOpenAPISpec(tempDir, "Test API", "1.0.0")
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

func TestGenerateOpenAPISpecHonoursTitleAndVersion(t *testing.T) {
	dir := t.TempDir()
	src := `package rt

import "net/http"

func Setup(mux *http.ServeMux) {
	mux.HandleFunc("GET /items", listItems)
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	spec, err := GenerateOpenAPISpec(dir, "Store API", "2.1.0")
	if err != nil {
		t.Fatalf("GenerateOpenAPISpec failed: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("invalid spec JSON: %v", err)
	}
	info := doc["info"].(map[string]any)

	if info["title"] != "Store API" {
		t.Errorf("title = %v, want Store API", info["title"])
	}
	if info["version"] != "2.1.0" {
		t.Errorf("version = %v, want 2.1.0", info["version"])
	}
}

func TestGenerateOpenAPISpecDefaultsVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package rt\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	spec, err := GenerateOpenAPISpec(dir, "API", "")
	if err != nil {
		t.Fatalf("GenerateOpenAPISpec failed: %v", err)
	}
	if !strings.Contains(spec, `"version": "1.0.0"`) {
		t.Errorf("expected a default version of 1.0.0, got:\n%s", spec)
	}
}
