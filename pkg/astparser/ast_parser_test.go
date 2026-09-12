package astparser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestASTParser(t *testing.T) {
	tempDir := t.TempDir()

	src := `package models

type User struct {
	ID        uint      ` + "`" + `json:"id" gorm:"primaryKey" db:"id"` + "`" + `
	Email     string    ` + "`" + `json:"email" validate:"required,email" gorm:"uniqueIndex"` + "`" + `
	Name      string    ` + "`" + `json:"name"` + "`" + `
	IsActive  bool      ` + "`" + `json:"is_active"` + "`" + `
}

type UserRepository interface {
	FindByID(id uint) (*User, error)
	Create(u *User) error
}
`
	filePath := filepath.Join(tempDir, "user.go")
	if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	symbols, err := ParsePath(filePath)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	if symbols.Package != "models" {
		t.Errorf("expected package 'models', got '%s'", symbols.Package)
	}

	if len(symbols.Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(symbols.Structs))
	}
	userStruct := symbols.Structs[0]
	if userStruct.Name != "User" {
		t.Errorf("expected struct name 'User', got '%s'", userStruct.Name)
	}
	if len(userStruct.Fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(userStruct.Fields))
	}
	emailField := userStruct.Fields[1]
	if emailField.ParsedTags["validate"] != "required,email" {
		t.Errorf("expected validate tag 'required,email', got '%s'", emailField.ParsedTags["validate"])
	}

	if len(symbols.Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(symbols.Interfaces))
	}
	userRepo := symbols.Interfaces[0]
	if userRepo.Name != "UserRepository" {
		t.Errorf("expected interface name 'UserRepository', got '%s'", userRepo.Name)
	}
	if len(userRepo.Methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(userRepo.Methods))
	}
}

func TestRouteScanner(t *testing.T) {
	tempDir := t.TempDir()

	src := `package router

import "net/http"

func SetupRoutes(r AnyRouter, mux *http.ServeMux) {
	r.GET("/api/v1/users", HandleGetUsers)
	r.POST("/api/v1/users", AuthMiddleware, HandleCreateUser)
	mux.HandleFunc("GET /health", HandleHealth)
	mux.HandleFunc("POST /items/{id}", HandleUpdateItem)
}
`
	filePath := filepath.Join(tempDir, "routes.go")
	if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	routes, err := ScanRoutes(tempDir)
	if err != nil {
		t.Fatalf("ScanRoutes failed: %v", err)
	}

	if len(routes) != 4 {
		t.Fatalf("expected 4 routes, got %d", len(routes))
	}

	r0 := routes[0]
	if r0.Method != "GET" || r0.Path != "/api/v1/users" {
		t.Errorf("unexpected route 0: %+v", r0)
	}

	r1 := routes[1]
	if r1.Method != "POST" || r1.Path != "/api/v1/users" {
		t.Errorf("unexpected route 1: %+v", r1)
	}
	if len(r1.Middlewares) != 1 || r1.Middlewares[0] != "AuthMiddleware" {
		t.Errorf("expected AuthMiddleware in r1, got %+v", r1.Middlewares)
	}

	r2 := routes[2]
	if r2.Method != "GET" || r2.Path != "/health" {
		t.Errorf("unexpected route 2: %+v", r2)
	}

	r3 := routes[3]
	if r3.Method != "POST" || r3.Path != "/items/{id}" {
		t.Errorf("unexpected route 3: %+v", r3)
	}
}

func TestRouteGroups(t *testing.T) {
	tempDir := t.TempDir()

	src := `package router

func Register(r AnyEngine) {
	v1 := r.Group("/api/v1")
	v1.GET("/users", GetUsers)
	v1.POST("/users", CreateUser)

	auth := v1.Group("/auth")
	auth.POST("/login", Login)
}
`
	filePath := filepath.Join(tempDir, "routes.go")
	if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	routes, err := ScanRoutes(tempDir)
	if err != nil {
		t.Fatalf("ScanRoutes failed: %v", err)
	}

	if len(routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(routes))
	}

	if routes[0].Path != "/api/v1/users" {
		t.Errorf("expected /api/v1/users, got %s", routes[0].Path)
	}
	if routes[2].Path != "/api/v1/auth/login" {
		t.Errorf("expected /api/v1/auth/login, got %s", routes[2].Path)
	}
}

func TestGenericsAndMethods(t *testing.T) {
	tempDir := t.TempDir()

	src := `package generic

type Result[T any] struct {
	Data T ` + "`" + `json:"data"` + "`" + `
}

type Container[K comparable, V any] struct {
	Items map[K]Result[V] ` + "`" + `json:"items"` + "`" + `
}

func (c *Container[K, V]) Get(key K) Result[V] {
	return c.Items[key]
}
`
	filePath := filepath.Join(tempDir, "generic.go")
	if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	symbols, err := ParsePath(filePath)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	if len(symbols.Structs) != 2 {
		t.Fatalf("expected 2 structs, got %d", len(symbols.Structs))
	}

	container := symbols.Structs[1]
	if len(container.Methods) != 1 || container.Methods[0] != "Get" {
		t.Errorf("expected method 'Get' on Container, got %+v", container.Methods)
	}
}
