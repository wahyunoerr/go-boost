package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateReadOnlyQuery(t *testing.T) {
	validQueries := []string{
		"SELECT * FROM users WHERE id = 1",
		"select id, name from products",
		"SHOW TABLES",
		"EXPLAIN ANALYZE SELECT * FROM orders",
		"DESCRIBE users",
		"PRAGMA table_info(users)",
		"/* comment */ SELECT count(*) FROM logs -- trailing comment",
		"SELECT * FROM orders WHERE status = 'UPDATE'",
		"SELECT * FROM logs WHERE message = 'hello;world'",
	}

	for _, q := range validQueries {
		cleaned, err := ValidateReadOnlyQuery(q)
		if err != nil {
			t.Errorf("expected query '%s' to be valid, got error: %v", q, err)
		}
		if cleaned == "" {
			t.Errorf("expected cleaned query, got empty for '%s'", q)
		}
	}

	invalidQueries := []string{
		"INSERT INTO users (name) VALUES ('alice')",
		"UPDATE users SET name = 'bob' WHERE id = 1",
		"DELETE FROM users WHERE id = 1",
		"DROP TABLE users",
		"ALTER TABLE users ADD COLUMN age INT",
		"TRUNCATE TABLE logs",
		"SELECT * FROM users; DROP TABLE users;",
		"CREATE TABLE evil (id int)",
	}

	for _, q := range invalidQueries {
		_, err := ValidateReadOnlyQuery(q)
		if err == nil {
			t.Errorf("expected query '%s' to be rejected, but it was accepted", q)
		}
	}
}

func TestDiscoverConnections(t *testing.T) {
	tempDir := t.TempDir()

	envContent := `DB_CONNECTION=postgres
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=app_production
DB_USERNAME=postgres_user
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	conns, err := DiscoverConnections(tempDir)
	if err != nil {
		t.Fatalf("DiscoverConnections failed: %v", err)
	}

	if len(conns.Connections) == 0 {
		t.Fatalf("expected at least 1 connection found, got 0")
	}

	c0 := conns.Connections[0]
	if c0.Driver != "postgres" {
		t.Errorf("expected driver 'postgres', got '%s'", c0.Driver)
	}
	if c0.Database != "app_production" {
		t.Errorf("expected database 'app_production', got '%s'", c0.Database)
	}
	if c0.Username != "postgres_user" {
		t.Errorf("expected username 'postgres_user', got '%s'", c0.Username)
	}
}
