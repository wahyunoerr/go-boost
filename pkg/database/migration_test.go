package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wahyunoerr/go-boost/v2/pkg/astparser"
)

func TestMigrationScaffoldGeneration(t *testing.T) {
	tempDir := t.TempDir()

	src := `package domain

type Product struct {
	ID    int64   ` + "`" + `db:"id"` + "`" + `
	Name  string  ` + "`" + `db:"name"` + "`" + `
	Price float64 ` + "`" + `db:"price"` + "`" + `
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "product.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write product.go: %v", err)
	}

	conn := &DBConnection{
		Name:     "test_db",
		Driver:   "sqlite",
		Database: filepath.Join(tempDir, "test.db"),
	}

	tbl := TableDetail{
		Name: "products",
		Columns: []ColumnDetail{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR(255)"},
		},
	}
	st := astparser.StructInfo{
		Name: "Product",
		Fields: []astparser.FieldInfo{
			{Name: "ID", Type: "int64", ParsedTags: map[string]string{"db": "id"}},
			{Name: "Name", Type: "string", ParsedTags: map[string]string{"db": "name"}},
			{Name: "Price", Type: "float64", ParsedTags: map[string]string{"db": "price"}},
		},
	}

	diff := compareTableAndStruct(tbl, st)
	if len(diff.MissingInDatabase) != 1 || diff.MissingInDatabase[0] != "price" {
		t.Fatalf("expected 'price' to be missing in database, got %v", diff.MissingInDatabase)
	}

	upStmt := "ALTER TABLE " + diff.TableName + " ADD COLUMN " + diff.MissingInDatabase[0] + " DECIMAL(12,2);"
	downStmt := "ALTER TABLE " + diff.TableName + " DROP COLUMN " + diff.MissingInDatabase[0] + ";"

	if !strings.Contains(upStmt, "ADD COLUMN price") {
		t.Errorf("expected ADD COLUMN price in upStmt")
	}
	if !strings.Contains(downStmt, "DROP COLUMN price") {
		t.Errorf("expected DROP COLUMN price in downStmt")
	}

	_ = context.Background()
	_ = conn
}

func TestSanitizeMigrationName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty falls back", "", "sync_schema"},
		{"plain name", "add_users_table", "add_users_table"},
		{"spaces become underscores", "add users table", "add_users_table"},
		{"uppercase is lowered", "AddUsers", "addusers"},
		{"path separators are stripped", "../../etc/passwd", "etcpasswd"},
		{"punctuation is stripped", "add-users!@#", "add_users"},
		{"only punctuation falls back", "!!!", "sync_schema"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeMigrationName(tc.input); got != tc.want {
				t.Errorf("sanitizeMigrationName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
