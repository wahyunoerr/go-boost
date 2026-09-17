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

func TestScanCodebaseDistinguishesBaseDirectoryFromUntrustedSegment(t *testing.T) {
	vulns := scanSource(t, `package main

import (
	"os"
	"path/filepath"
)

func safe(rootDir string) {
	_, _ = os.ReadFile(filepath.Join(rootDir, "go.mod"))
	_, _ = os.ReadFile(filepath.Join(rootDir, "config", "app.yaml"))
}

func main() {}
`)

	if hasType(vulns, "path_traversal") {
		t.Errorf("joining a base directory with constant segments cannot traverse, got:\n%s", describe(vulns))
	}
}

func TestScanCodebaseStillFlagsUntrustedPathSegment(t *testing.T) {
	vulns := scanSource(t, `package main

import (
	"os"
	"path/filepath"
)

func unsafe(rootDir, userInput string) {
	_, _ = os.ReadFile(filepath.Join(rootDir, userInput))
}

func main() {}
`)

	if !hasType(vulns, "path_traversal") {
		t.Errorf("an untrusted trailing segment must still be reported, got:\n%s", describe(vulns))
	}
}

func TestSecretValueRegexCoversModernTokenFormats(t *testing.T) {
	alnum := func(n int) string { return strings.Repeat("a1B2c3D4e5", 8)[:n] }

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"openai project key", "sk" + "-proj-" + alnum(24) + "_XY", true},
		{"openai legacy key", "sk" + "-" + alnum(26), true},
		{"aws access key", "AKIA" + strings.ToUpper(alnum(16)), true},
		{"github personal token", "ghp" + "_" + alnum(36), true},
		{"github server token", "ghs" + "_" + alnum(36), true},
		{"gitlab token", "glpat" + "-" + alnum(20), true},
		{"google api key", "AIza" + alnum(35), true},
		{"stripe live key", "sk" + "_live_" + alnum(24), true},
		{"npm token", "npm" + "_" + alnum(36), true},
		{"slack token", "xoxb" + "-" + alnum(12), true},
		{"jwt", "eyJ" + alnum(10) + "." + alnum(10) + "." + alnum(6), true},
		{"private key header", "-----BEGIN RSA PRIVATE" + " KEY-----", true},
		{"ordinary sentence", "this is just a normal string value", false},
		{"short prefix only", "sk" + "-short", false},
		{"module path", "github.com/wahyunoerr/go-boost/v2/pkg/security", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretValueRegex.MatchString(tc.value); got != tc.want {
				t.Errorf("match(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestScanCodebaseDetectsCredentialSplitAcrossLiterals(t *testing.T) {
	vulns := scanSource(t, `package main

const token = "sk`+`_live_" + "a1B2c3D4e5a1B2c3D4e5a1B2"

func main() { _ = token }
`)

	if !hasType(vulns, "hardcoded_secret") {
		t.Errorf("a key split across concatenated literals must still be detected, got:\n%s", describe(vulns))
	}
}

func TestScanCodebaseDetectsOpenAIProjectKey(t *testing.T) {
	vulns := scanSource(t, `package main

const token = "sk" + "-proj-" + "a1B2c3D4e5a1B2c3D4e5a1B2" + "_XY"

func main() { _ = token }
`)

	if !hasType(vulns, "hardcoded_secret") {
		t.Errorf("a modern OpenAI project key must be detected, got:\n%s", describe(vulns))
	}
}

func TestBaselineAcceptsKnownFindingsButNotNewOnes(t *testing.T) {
	dir := t.TempDir()
	source := `package main

const apiKey = "abcdefghijklmnopqrstuvwxyz123456"

func main() { _ = apiKey }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	first, err := ScanCodebase(dir)
	if err != nil {
		t.Fatalf("ScanCodebase failed: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected a finding to record")
	}
	if _, err := WriteBaseline(dir, first); err != nil {
		t.Fatalf("WriteBaseline failed: %v", err)
	}

	baseline, err := LoadBaseline(dir)
	if err != nil {
		t.Fatalf("LoadBaseline failed: %v", err)
	}
	remaining, accepted := ApplyBaseline(first, baseline)
	if len(remaining) != 0 {
		t.Errorf("recorded findings should be accepted, got %d remaining", len(remaining))
	}
	if accepted != len(first) {
		t.Errorf("accepted = %d, want %d", accepted, len(first))
	}

	newSource := source + `
const jwtSecret = "another-long-production-secret"
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(newSource), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	second, err := ScanCodebase(dir)
	if err != nil {
		t.Fatalf("ScanCodebase failed: %v", err)
	}
	remaining, _ = ApplyBaseline(second, baseline)
	if len(remaining) == 0 {
		t.Fatal("a baseline must never hide a finding that was not recorded in it")
	}
}

func TestLoadBaselineWithoutFileIsNotAnError(t *testing.T) {
	b, err := LoadBaseline(t.TempDir())
	if err != nil {
		t.Fatalf("expected a missing baseline to be fine, got: %v", err)
	}
	if len(b.Accepted) != 0 {
		t.Errorf("expected an empty baseline, got %d entries", len(b.Accepted))
	}
}
