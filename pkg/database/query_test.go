package database

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReadOnlyQueryAcceptsReads(t *testing.T) {
	valid := []string{
		"SELECT * FROM users WHERE id = 1",
		"select id, name from products",
		"SHOW TABLES",
		"EXPLAIN ANALYZE SELECT * FROM orders",
		"DESCRIBE users",
		"PRAGMA table_info(users)",
		"/* comment */ SELECT count(*) FROM logs -- trailing comment",
		"SELECT * FROM orders WHERE status = 'UPDATE'",
		"SELECT * FROM logs WHERE message = 'hello;world'",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
	}

	for _, q := range valid {
		t.Run(q, func(t *testing.T) {
			cleaned, err := ValidateReadOnlyQuery(q)
			if err != nil {
				t.Fatalf("expected query to be accepted, got error: %v", err)
			}
			if cleaned == "" {
				t.Fatal("expected a cleaned query, got empty string")
			}
		})
	}
}

func TestValidateReadOnlyQueryRejectsWrites(t *testing.T) {
	invalid := []struct {
		name  string
		query string
	}{
		{"insert", "INSERT INTO users (name) VALUES ('alice')"},
		{"update", "UPDATE users SET name = 'bob' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"drop", "DROP TABLE users"},
		{"alter", "ALTER TABLE users ADD COLUMN age INT"},
		{"truncate", "TRUNCATE TABLE logs"},
		{"stacked", "SELECT * FROM users; DROP TABLE users;"},
		{"create", "CREATE TABLE evil (id int)"},

		{"select into creates a table", "SELECT * INTO stolen_copy FROM users"},
		{"pragma assignment writes", "PRAGMA user_version = 1337"},
		{"load_extension runs native code", "SELECT load_extension('/tmp/evil.so')"},
		{"into outfile writes a file", "SELECT 'x' INTO OUTFILE '/var/www/shell.php'"},
		{"setval mutates a sequence", "SELECT setval('orders_id_seq', 1)"},
		{"pg_terminate_backend kills sessions", "SELECT pg_terminate_backend(pid) FROM pg_stat_activity"},
		{"pg_read_file reads the filesystem", "SELECT pg_read_file('/etc/passwd')"},
		{"lo_export writes the filesystem", "SELECT lo_export(1, '/tmp/out')"},
		{"attach mounts another database", "PRAGMA foo; ATTACH DATABASE '/tmp/x.db' AS x"},
		{"copy is a bulk write", "COPY users FROM '/tmp/users.csv'"},
		{"benchmark burns cpu", "SELECT benchmark(100000000, md5('a'))"},
		{"pg_sleep stalls the connection", "SELECT pg_sleep(30)"},
		{"comment hides a stacked write", "SELECT 1 --\nDROP TABLE users"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateReadOnlyQuery(tc.query); err == nil {
				t.Fatalf("expected query to be rejected: %s", tc.query)
			}
		})
	}
}

func TestValidateReadOnlyQueryPreservesStringLiterals(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "double dash inside literal",
			query: "SELECT * FROM notes WHERE body LIKE '%--%' AND id = 1",
			want:  "SELECT * FROM notes WHERE body LIKE '%--%' AND id = 1",
		},
		{
			name:  "block comment marker inside literal",
			query: "SELECT * FROM notes WHERE body = '/* not a comment */'",
			want:  "SELECT * FROM notes WHERE body = '/* not a comment */'",
		},
		{
			name:  "escaped quote inside literal",
			query: "SELECT * FROM users WHERE name = 'O''Brien'",
			want:  "SELECT * FROM users WHERE name = 'O''Brien'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateReadOnlyQuery(tc.query)
			if err != nil {
				t.Fatalf("expected query to be accepted, got error: %v", err)
			}
			if got != tc.want {
				t.Errorf("query was corrupted\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestExecuteQueryCannotWriteToSQLite(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not installed")
	}

	dbPath := filepath.Join(t.TempDir(), "app.db")
	if err := exec.Command("sqlite3", dbPath, "CREATE TABLE users (id INTEGER PRIMARY KEY); PRAGMA user_version = 1;").Run(); err != nil {
		t.Fatalf("failed to seed sqlite database: %v", err)
	}

	conn := &DBConnection{Driver: "sqlite", Database: dbPath}

	cmd := exec.Command("sqlite3", "-readonly", dbPath, "PRAGMA user_version = 1337;")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("sqlite accepted a write in read-only mode: %s", out)
	}

	out, err := ExecuteQuery(context.Background(), conn, "PRAGMA user_version")
	if err != nil {
		t.Fatalf("read-only query failed: %v", err)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("expected user_version to still be 1, got %q", out)
	}
}

func TestExecuteQueryRejectsMissingConnection(t *testing.T) {
	if _, err := ExecuteQuery(context.Background(), nil, "SELECT 1"); err == nil {
		t.Fatal("expected an error when no connection is configured")
	}
}

func TestDiscoverConnections(t *testing.T) {
	tempDir := t.TempDir()

	envContent := `DB_CONNECTION=postgres
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=app_production
DB_USERNAME=postgres_user
DB_PASSWORD=hunter2
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	conns, err := DiscoverConnections(tempDir)
	if err != nil {
		t.Fatalf("DiscoverConnections failed: %v", err)
	}
	if len(conns.Connections) == 0 {
		t.Fatal("expected at least 1 connection, got 0")
	}

	c0 := conns.Connections[0]
	if c0.Driver != "postgres" {
		t.Errorf("expected driver 'postgres', got %q", c0.Driver)
	}
	if c0.Database != "app_production" {
		t.Errorf("expected database 'app_production', got %q", c0.Database)
	}
	if c0.Username != "postgres_user" {
		t.Errorf("expected username 'postgres_user', got %q", c0.Username)
	}
	if c0.Password != "hunter2" {
		t.Errorf("expected the password to be available for connecting, got %q", c0.Password)
	}
}

func TestDiscoveredConnectionsNeverSerializePasswords(t *testing.T) {
	tempDir := t.TempDir()

	envContent := `DATABASE_URL=postgres://admin:S3cr3tP4ss@db.prod:5432/app
MYSQL_URL=root:rootpw@tcp(127.0.0.1:3306)/shop
DB_CONNECTION=mysql
DB_DATABASE=app
DB_PASSWORD=another-secret
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	conns, err := DiscoverConnections(tempDir)
	if err != nil {
		t.Fatalf("DiscoverConnections failed: %v", err)
	}

	serialized := ToJSON(conns)
	for _, secret := range []string{"S3cr3tP4ss", "rootpw", "another-secret"} {
		if strings.Contains(serialized, secret) {
			t.Errorf("password %q leaked into serialized output:\n%s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "***") {
		t.Errorf("expected the DSN to be redacted, got:\n%s", serialized)
	}
}

func TestParseDSN(t *testing.T) {
	tests := []struct {
		name         string
		dsn          string
		wantDriver   string
		wantHost     string
		wantPort     string
		wantDatabase string
		wantUsername string
		wantPassword string
	}{
		{
			name:       "postgres url",
			dsn:        "postgres://admin:secret@db.prod:5432/app",
			wantDriver: "postgres", wantHost: "db.prod", wantPort: "5432",
			wantDatabase: "app", wantUsername: "admin", wantPassword: "secret",
		},
		{
			name:       "go mysql driver dsn",
			dsn:        "root:rootpw@tcp(127.0.0.1:3306)/shop",
			wantDriver: "mysql", wantHost: "127.0.0.1", wantPort: "3306",
			wantDatabase: "shop", wantUsername: "root", wantPassword: "rootpw",
		},
		{
			name:       "mysql dsn without password",
			dsn:        "app@tcp(db:3306)/shop?parseTime=true",
			wantDriver: "mysql", wantHost: "db", wantPort: "3306",
			wantDatabase: "shop", wantUsername: "app",
		},
		{
			name:       "postgres key value dsn",
			dsn:        "host=localhost port=5432 dbname=app user=postgres password=secret",
			wantDriver: "postgres", wantHost: "localhost", wantPort: "5432",
			wantDatabase: "app", wantUsername: "postgres", wantPassword: "secret",
		},
		{
			name:       "postgresql scheme variant",
			dsn:        "postgresql://user@localhost/app",
			wantDriver: "postgres", wantHost: "localhost",
			wantDatabase: "app", wantUsername: "user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn, ok := parseDSN(tc.dsn, "test")
			if !ok {
				t.Fatalf("failed to parse DSN %q", tc.dsn)
			}
			if conn.Driver != tc.wantDriver {
				t.Errorf("driver = %q, want %q", conn.Driver, tc.wantDriver)
			}
			if conn.Host != tc.wantHost {
				t.Errorf("host = %q, want %q", conn.Host, tc.wantHost)
			}
			if conn.Port != tc.wantPort {
				t.Errorf("port = %q, want %q", conn.Port, tc.wantPort)
			}
			if conn.Database != tc.wantDatabase {
				t.Errorf("database = %q, want %q", conn.Database, tc.wantDatabase)
			}
			if conn.Username != tc.wantUsername {
				t.Errorf("username = %q, want %q", conn.Username, tc.wantUsername)
			}
			if conn.Password != tc.wantPassword {
				t.Errorf("password = %q, want %q", conn.Password, tc.wantPassword)
			}
			if tc.wantPassword != "" && strings.Contains(conn.DSN, tc.wantPassword) {
				t.Errorf("redacted DSN still contains the password: %q", conn.DSN)
			}
		})
	}
}

func TestDiscoverConnectionsPrefersProjectEnvOverShell(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://shell@shell-host:5432/shell_db")

	tempDir := t.TempDir()
	envContent := "DATABASE_URL=postgres://app@project-host:5432/project_db\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	conns, err := DiscoverConnections(tempDir)
	if err != nil {
		t.Fatalf("DiscoverConnections failed: %v", err)
	}
	if len(conns.Connections) == 0 {
		t.Fatal("expected at least one connection")
	}
	if got := conns.Connections[0].Database; got != "project_db" {
		t.Errorf("default connection = %q, want the project's own database %q", got, "project_db")
	}
}

func TestSafeArgValueNeutralizesFlagLookalikes(t *testing.T) {
	if got := safeArgValue("-readonly"); got != "./-readonly" {
		t.Errorf("safeArgValue(%q) = %q, want a value that cannot be read as a flag", "-readonly", got)
	}
	if got := safeArgValue("app.db"); got != "app.db" {
		t.Errorf("safeArgValue(%q) = %q, want it unchanged", "app.db", got)
	}
}
