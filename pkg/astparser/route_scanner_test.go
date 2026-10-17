package astparser

import (
	"os"
	"path/filepath"
	"testing"
)

func scanRoutesIn(t *testing.T, files map[string]string) []RouteInfo {
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

	routes, err := ScanRoutes(dir)
	if err != nil {
		t.Fatalf("ScanRoutes failed: %v", err)
	}
	return routes
}

func findRoute(routes []RouteInfo, method, path string) *RouteInfo {
	for i := range routes {
		if routes[i].Method == method && routes[i].Path == path {
			return &routes[i]
		}
	}
	return nil
}

func TestScanRoutesHandlesVerbFirstRegistration(t *testing.T) {
	routes := scanRoutesIn(t, map[string]string{
		"routes.go": `package rt

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.Handle("POST", "/login", Login)
	r.Handle("GET", "/health", Health)
}
`,
	})

	if r := findRoute(routes, "POST", "/login"); r == nil {
		t.Errorf("expected POST /login to be detected, got %+v", routes)
	} else if r.Handler != "Login" {
		t.Errorf("handler = %q, want Login", r.Handler)
	}
	if findRoute(routes, "GET", "/health") == nil {
		t.Errorf("expected GET /health to be detected, got %+v", routes)
	}
	for _, r := range routes {
		if r.Path == "POST" || r.Path == "GET" {
			t.Errorf("the HTTP verb was recorded as a path: %+v", r)
		}
	}
}

func TestScanRoutesIgnoresNonRouterCalls(t *testing.T) {
	routes := scanRoutesIn(t, map[string]string{
		"cache.go": `package rt

func Warm(cache Cache, store Store) {
	var v string
	cache.Get("session-key", &v)
	store.Delete("tenant:42", true)
}
`,
	})

	if len(routes) != 0 {
		t.Errorf("expected no routes from cache and store calls, got %+v", routes)
	}
}

func TestScanRoutesResolvesGroupPrefixAcrossFunctions(t *testing.T) {
	routes := scanRoutesIn(t, map[string]string{
		"main.go": `package rt

import "github.com/gin-gonic/gin"

func Setup(r *gin.Engine) {
	v1 := r.Group("/api/v1")
	RegisterUserRoutes(v1)
}
`,
		"users.go": `package rt

import "github.com/gin-gonic/gin"

func RegisterUserRoutes(rg *gin.RouterGroup) {
	rg.GET("/users", ListUsers)
	rg.POST("/users", CreateUser)
}
`,
	})

	if findRoute(routes, "GET", "/api/v1/users") == nil {
		t.Errorf("expected the caller's group prefix to be applied, got %+v", routes)
	}
	if findRoute(routes, "POST", "/api/v1/users") == nil {
		t.Errorf("expected POST /api/v1/users, got %+v", routes)
	}
}

func TestScanRoutesKeepsInlineHandlersIdentifiable(t *testing.T) {
	routes := scanRoutesIn(t, map[string]string{
		"routes.go": `package rt

import "net/http"

func Setup(mux *http.ServeMux) {
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {})
}
`,
	})

	r := findRoute(routes, "GET", "/ping")
	if r == nil {
		t.Fatalf("expected GET /ping, got %+v", routes)
	}
	if r.Handler == "unknown" {
		t.Errorf("expected an inline handler to be labelled, got %q", r.Handler)
	}
}

func TestScanRoutesIsDeterministic(t *testing.T) {
	files := map[string]string{
		"a.go": "package rt\n\nfunc A(r Router) { r.GET(\"/a\", HA) }\n",
		"b.go": "package rt\n\nfunc B(r Router) { r.GET(\"/b\", HB) }\n",
		"c.go": "package rt\n\nfunc C(r Router) { r.GET(\"/c\", HC) }\n",
	}

	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	first, err := ScanRoutes(dir)
	if err != nil {
		t.Fatalf("ScanRoutes failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := ScanRoutes(dir)
		if err != nil {
			t.Fatalf("ScanRoutes failed: %v", err)
		}
		if len(next) != len(first) {
			t.Fatalf("route count changed between runs")
		}
		for j := range next {
			if next[j].Path != first[j].Path {
				t.Fatalf("route order changed between runs at %d", j)
			}
		}
	}
}
