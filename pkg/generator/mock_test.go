package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMockCode(t *testing.T) {
	tempDir := t.TempDir()

	src := `package service

type PaymentGateway interface {
	Charge(amount float64, currency string) (string, error)
	Refund(transactionID string) error
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "payment.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write payment.go: %v", err)
	}

	mockCode, err := GenerateMockCode(tempDir, "PaymentGateway")
	if err != nil {
		t.Fatalf("GenerateMockCode failed: %v", err)
	}

	for _, want := range []string{
		"type MockPaymentGateway struct",
		"ChargeFunc func(amount float64, currency string) (string, error)",
		"func (m *MockPaymentGateway) Charge",
	} {
		if !strings.Contains(mockCode, want) {
			t.Errorf("expected generated code to contain %q\n\n%s", want, mockCode)
		}
	}
}

func generateAndBuild(t *testing.T, source, interfaceName string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module mocktest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iface.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	code, err := GenerateMockCode(dir, interfaceName)
	if err != nil {
		t.Fatalf("GenerateMockCode failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mock_gen.go"), []byte(code), 0o644); err != nil {
		t.Fatalf("failed to write mock: %v", err)
	}

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated mock does not compile: %v\n%s\n--- generated code ---\n%s", err, out, code)
	}

	return code
}

func TestGeneratedMockCompilesForAwkwardSignatures(t *testing.T) {
	code := generateAndBuild(t, `package mocktest

import (
	"context"
	"io"
	"time"
)

type Store interface {
	Get(context.Context, string) (io.Reader, error)
	Tags(names ...string) error
	Touch(id string, at time.Time)
	Timeout() time.Duration
	Handler() func(int) error
}
`, "Store")

	if !strings.Contains(code, "var _ Store = (*MockStore)(nil)") {
		t.Errorf("expected a compile-time interface assertion in the generated mock:\n%s", code)
	}
	if !strings.Contains(code, "names...") {
		t.Errorf("expected the variadic parameter to be forwarded with '...':\n%s", code)
	}
}

func TestGeneratedMockCompilesForEmbeddedLocalInterface(t *testing.T) {
	generateAndBuild(t, `package mocktest

import "context"

type Reader interface {
	Find(ctx context.Context, id string) (string, error)
}

type Writer interface {
	Save(ctx context.Context, value string) error
}

type Repository interface {
	Reader
	Writer
	Close() error
}
`, "Repository")
}

func TestGeneratedMockCompilesForGenericInterface(t *testing.T) {
	generateAndBuild(t, `package mocktest

import "context"

type Repository[T any] interface {
	Find(ctx context.Context, id string) (T, error)
	Save(ctx context.Context, item T) error
}
`, "Repository")
}

func TestGenerateMockCodeExplainsCrossPackageEmbedding(t *testing.T) {
	dir := t.TempDir()
	src := `package service

import "io"

type Blob interface {
	io.Reader
	Name() string
}
`
	if err := os.WriteFile(filepath.Join(dir, "blob.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	_, err := GenerateMockCode(dir, "Blob")
	if err == nil {
		t.Fatal("expected an explanatory error for a cross-package embedded interface")
	}
	if !strings.Contains(err.Error(), "io.Reader") {
		t.Errorf("expected the error to name the embedded interface, got: %v", err)
	}
}

func TestGenerateMockCodeReportsMissingInterface(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\ntype S struct{}\n"), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if _, err := GenerateMockCode(dir, "Nope"); err == nil {
		t.Fatal("expected an error for an unknown interface")
	}
}
