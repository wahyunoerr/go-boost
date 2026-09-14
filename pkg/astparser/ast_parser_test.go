package astparser

import (
	"os"
	"path/filepath"
	"strings"
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

	container := findStruct(t, symbols, "Container")
	if len(container.Methods) != 1 || container.Methods[0] != "Get" {
		t.Errorf("expected method 'Get' on Container, got %+v", container.Methods)
	}
	if len(container.TypeParams) != 2 {
		t.Errorf("expected 2 type parameters on Container, got %+v", container.TypeParams)
	}
}

func findStruct(t *testing.T, symbols *PackageSymbols, name string) StructInfo {
	t.Helper()
	for _, s := range symbols.Structs {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("struct %q not found", name)
	return StructInfo{}
}

func findInterfaceInfo(t *testing.T, symbols *PackageSymbols, name string) InterfaceInfo {
	t.Helper()
	for _, i := range symbols.Interfaces {
		if i.Name == name {
			return i
		}
	}
	t.Fatalf("interface %q not found", name)
	return InterfaceInfo{}
}

func TestParsePathRecordsEveryDeclaredFieldName(t *testing.T) {
	dir := t.TempDir()
	src := `package models

type Rect struct {
	Width, Height int ` + "`" + `db:"size"` + "`" + `
	Label         string
}
`
	if err := os.WriteFile(filepath.Join(dir, "rect.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	symbols, err := ParsePath(dir)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	rect := findStruct(t, symbols, "Rect")
	if len(rect.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d: %+v", len(rect.Fields), rect.Fields)
	}
	names := []string{rect.Fields[0].Name, rect.Fields[1].Name, rect.Fields[2].Name}
	for i, want := range []string{"Width", "Height", "Label"} {
		if names[i] != want {
			t.Errorf("field %d = %q, want %q", i, names[i], want)
		}
	}
}

func TestParsePathFindsMethodsDeclaredInOtherFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte("package svc\n\ntype Service struct{}\n"), 0644); err != nil {
		t.Fatalf("failed to write model: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handlers.go"), []byte("package svc\n\nfunc (s *Service) Run() error { return nil }\nfunc (s *Service) Stop() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write handlers: %v", err)
	}

	symbols, err := ParsePath(dir)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	svc := findStruct(t, symbols, "Service")
	if len(svc.Methods) != 2 {
		t.Fatalf("expected 2 methods from another file, got %+v", svc.Methods)
	}
}

func TestParsePathRecordsEmbeddedInterfaces(t *testing.T) {
	dir := t.TempDir()
	src := `package repo

import "io"

type Base interface {
	Ping() error
}

type Store interface {
	Base
	io.Closer
	Get(id string) (string, error)
}
`
	if err := os.WriteFile(filepath.Join(dir, "store.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	symbols, err := ParsePath(dir)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	store := findInterfaceInfo(t, symbols, "Store")
	if len(store.Embeds) != 2 {
		t.Fatalf("expected 2 embedded interfaces, got %+v", store.Embeds)
	}
	if len(store.Methods) != 1 {
		t.Errorf("expected 1 directly declared method, got %+v", store.Methods)
	}
}

func TestParsePathSynthesisesParameterNames(t *testing.T) {
	dir := t.TempDir()
	src := `package repo

import "context"

type Store interface {
	Get(context.Context, string) (string, error)
	Tags(names ...string) error
	Pair() (a, b int)
}
`
	if err := os.WriteFile(filepath.Join(dir, "store.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	symbols, err := ParsePath(dir)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	store := findInterfaceInfo(t, symbols, "Store")
	byName := map[string]InterfaceMethod{}
	for _, m := range store.Methods {
		byName[m.Name] = m
	}

	get := byName["Get"]
	if len(get.ParamNames) != 2 || get.ParamNames[0] == "" {
		t.Errorf("expected synthesised parameter names for Get, got %+v", get.ParamNames)
	}

	if !byName["Tags"].Variadic {
		t.Error("expected Tags to be marked variadic")
	}

	if got := len(byName["Pair"].Returns); got != 2 {
		t.Errorf("expected 2 return values for Pair, got %d", got)
	}
}

func TestParsePathIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		src := "package p\n\ntype S" + strings.ToUpper(name) + " struct{ F int }\n"
		if err := os.WriteFile(filepath.Join(dir, name+".go"), []byte(src), 0644); err != nil {
			t.Fatalf("failed to write source: %v", err)
		}
	}

	first, err := ParsePath(dir)
	if err != nil {
		t.Fatalf("ParsePath failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		next, err := ParsePath(dir)
		if err != nil {
			t.Fatalf("ParsePath failed: %v", err)
		}
		for j := range next.Structs {
			if next.Structs[j].Name != first.Structs[j].Name {
				t.Fatalf("struct order changed between runs at index %d: %s then %s",
					j, first.Structs[j].Name, next.Structs[j].Name)
			}
		}
	}
}
