package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scanSource(t *testing.T, source string) []SecurityVulnerability {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	vulns, err := ScanCodebase(dir)
	if err != nil {
		t.Fatalf("ScanCodebase failed: %v", err)
	}
	return vulns
}

func hasType(vulns []SecurityVulnerability, kind string) bool {
	for _, v := range vulns {
		if v.Type == kind {
			return true
		}
	}
	return false
}

func describe(vulns []SecurityVulnerability) string {
	var sb strings.Builder
	for _, v := range vulns {
		sb.WriteString(v.Type + " @" + v.File + ": " + v.Message + "\n")
	}
	if sb.Len() == 0 {
		return "(no findings)"
	}
	return sb.String()
}

func TestScanCodebaseFindsCommonRealWorldVulnerabilities(t *testing.T) {
	vulns := scanSource(t, `package main

import (
	"crypto/tls"
	"database/sql"
	"os/exec"
)

const apiKey = "sk-proj-9f8a7s6d5f4g3h2j1k"

var jwtSecret = "super-long-production-secret-value"

func run(db *sql.DB, userInput string) {
	exec.Command("sh", "-c", userInput).Run()

	q := "SELECT * FROM users WHERE name = '" + userInput + "'"
	db.Query(q)

	cfg := &tls.Config{}
	cfg.InsecureSkipVerify = true
	_ = cfg
}

func main() {}
`)

	tests := []struct {
		kind string
		why  string
	}{
		{"hardcoded_secret", "a credential in a const or var declaration"},
		{"command_injection", "a caller-controlled argument passed to sh -c"},
		{"sql_injection", "a query assembled into a variable before being executed"},
		{"insecure_tls", "InsecureSkipVerify enabled through an assignment"},
	}

	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			if !hasType(vulns, tc.kind) {
				t.Errorf("expected to detect %s.\nFindings were:\n%s", tc.why, describe(vulns))
			}
		})
	}
}

func TestScanCodebaseFindsDirectPatterns(t *testing.T) {
	vulns := scanSource(t, `package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"os"
)

func direct(ctx context.Context, db *sql.DB, id string) {
	db.QueryContext(ctx, "SELECT * FROM orders WHERE id = "+id)
	db.Exec(fmt.Sprintf("DELETE FROM logs WHERE owner = %s", id))
	_, _ = os.ReadFile("/var/data/" + id)
	_ = &tls.Config{InsecureSkipVerify: true}
}

func main() {}
`)

	for _, kind := range []string{"sql_injection", "path_traversal", "insecure_tls"} {
		if !hasType(vulns, kind) {
			t.Errorf("expected %s to be detected.\nFindings were:\n%s", kind, describe(vulns))
		}
	}
}

func TestScanCodebaseDoesNotFlagSafeCode(t *testing.T) {
	vulns := scanSource(t, `package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"os"
	"os/exec"
)

const defaultTimeout = "30s"

func safe(ctx context.Context, db *sql.DB, id string) error {
	if _, err := db.QueryContext(ctx, "SELECT * FROM orders WHERE id = $1", id); err != nil {
		return err
	}
	if err := exec.Command("git", "status", id).Run(); err != nil {
		return err
	}
	if _, err := os.ReadFile("config/app.yaml"); err != nil {
		return err
	}
	_ = &tls.Config{InsecureSkipVerify: false}
	_ = defaultTimeout
	return nil
}

func main() {}
`)

	if len(vulns) != 0 {
		t.Errorf("expected no findings for safe code, got:\n%s", describe(vulns))
	}
}

func TestScanCodebaseIgnoresPlaceholderSecrets(t *testing.T) {
	vulns := scanSource(t, `package main

const apiKey = "changeme"

var dbPassword = "test"

func main() {}
`)

	if hasType(vulns, "hardcoded_secret") {
		t.Errorf("expected placeholders to be ignored, got:\n%s", describe(vulns))
	}
}

func TestScanCodebaseReportsUnparseableFiles(t *testing.T) {
	vulns := scanSource(t, "package main\n\nfunc broken( {\n")

	if !hasType(vulns, "scan_error") {
		t.Errorf("expected an unparseable file to be reported, got:\n%s", describe(vulns))
	}
}

func TestScanCodebaseReportsRelativePaths(t *testing.T) {
	vulns := scanSource(t, `package main

const apiKey = "sk-proj-9f8a7s6d5f4g3h2j1k"

func main() {}
`)

	if len(vulns) == 0 {
		t.Fatal("expected at least one finding")
	}
	for _, v := range vulns {
		if filepath.IsAbs(v.File) {
			t.Errorf("expected a repository-relative path, got %q", v.File)
		}
	}
}
