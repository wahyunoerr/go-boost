package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityScanner(t *testing.T) {
	tempDir := t.TempDir()

	src := `package service

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
)

func InsecureOperations(ctx context.Context, db *sql.DB, id string) {
	db.Query("SELECT * FROM users WHERE id = " + id)
	db.QueryContext(ctx, "SELECT * FROM items WHERE id = " + id)
	db.Query(fmt.Sprintf("SELECT * FROM orders WHERE id = '%s'", id))
	_ = &tls.Config{
		InsecureSkipVerify: true,
	}
	apiKey := "AKIA1234567890ABCDEF"
	_ = apiKey
	_ = exec.Command("sh", "-c", "echo " + id)
	_, _ = os.Open("/var/data/" + id)
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write service.go: %v", err)
	}

	vulns, err := ScanCodebase(tempDir)
	if err != nil {
		t.Fatalf("ScanCodebase failed: %v", err)
	}

	var hasSQLi, hasInsecureTLS, hasSecret, hasCmdInj, hasPathTrav bool
	for _, v := range vulns {
		if v.Type == "sql_injection" {
			hasSQLi = true
		}
		if v.Type == "insecure_tls" {
			hasInsecureTLS = true
		}
		if v.Type == "hardcoded_secret" {
			hasSecret = true
		}
		if v.Type == "command_injection" {
			hasCmdInj = true
		}
		if v.Type == "path_traversal" {
			hasPathTrav = true
		}
	}

	if !hasSQLi {
		t.Errorf("expected SQL injection to be flagged")
	}
	if !hasInsecureTLS {
		t.Errorf("expected Insecure TLS to be flagged")
	}
	if !hasSecret {
		t.Errorf("expected hardcoded secret to be flagged")
	}
	if !hasCmdInj {
		t.Errorf("expected command injection to be flagged")
	}
	if !hasPathTrav {
		t.Errorf("expected path traversal to be flagged")
	}
}
