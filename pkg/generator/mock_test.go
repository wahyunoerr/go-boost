package generator

import (
	"os"
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

	if !strings.Contains(mockCode, "type MockPaymentGateway struct") {
		t.Errorf("expected struct MockPaymentGateway in generated code")
	}
	if !strings.Contains(mockCode, "ChargeFunc func(amount float64, currency string) (string, error)") {
		t.Errorf("expected ChargeFunc field in mock struct")
	}
	if !strings.Contains(mockCode, "func (m *MockPaymentGateway) Charge") {
		t.Errorf("expected Charge method implementation in mock struct")
	}
}
