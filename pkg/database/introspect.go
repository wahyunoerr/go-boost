package database

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type SchemaOptions struct {
	Summary              bool   `json:"summary"`
	Filter               string `json:"filter"`
	IncludeColumnDetails bool   `json:"include_column_details"`
	IncludeViews         bool   `json:"include_views"`
}

type TableDetail struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Columns     []ColumnDetail     `json:"columns"`
	Indexes     []IndexDetail      `json:"indexes,omitempty"`
	ForeignKeys []ForeignKeyDetail `json:"foreign_keys,omitempty"`
}

type ColumnDetail struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Nullable      bool   `json:"nullable,omitempty"`
	DefaultValue  string `json:"default_value,omitempty"`
	PrimaryKey    bool   `json:"primary_key,omitempty"`
	AutoIncrement bool   `json:"auto_increment,omitempty"`
}

type IndexDetail struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

type ForeignKeyDetail struct {
	Column           string `json:"column"`
	ReferencedTable  string `json:"referenced_table"`
	ReferencedColumn string `json:"referenced_column"`
}

type SchemaResult struct {
	Connection string        `json:"connection"`
	Driver     string        `json:"driver"`
	Database   string        `json:"database"`
	Tables     []TableDetail `json:"tables"`
}

func Introspect(ctx context.Context, conn *DBConnection, opts SchemaOptions) (*SchemaResult, error) {
	if conn == nil {
		return nil, fmt.Errorf("no database connection provided")
	}

	result := &SchemaResult{
		Connection: conn.Name,
		Driver:     conn.Driver,
		Database:   conn.Database,
		Tables:     make([]TableDetail, 0),
	}

	switch conn.Driver {
	case "sqlite":
		return introspectSQLite(ctx, conn, opts, result)
	case "postgres":
		return introspectPostgres(ctx, conn, opts, result)
	case "mysql":
		return introspectMySQL(ctx, conn, opts, result)
	default:
		return result, fmt.Errorf("unsupported driver: %s", conn.Driver)
	}
}

func introspectSQLite(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := "SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%';"
	cmd := exec.CommandContext(execCtx, "sqlite3", "-separator", "|", conn.Database, query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("failed to introspect sqlite: %v, out: %s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, l := range lines {
		parts := strings.Split(l, "|")
		if len(parts) < 2 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		tableType := strings.TrimSpace(parts[1])

		if !opts.IncludeViews && tableType == "view" {
			continue
		}

		if opts.Filter != "" && !strings.Contains(strings.ToLower(tableName), strings.ToLower(opts.Filter)) {
			continue
		}

		if !isValidSQLIdentifier(tableName) {
			continue
		}

		tbl := TableDetail{
			Name:    tableName,
			Type:    strings.ToUpper(tableType),
			Columns: make([]ColumnDetail, 0),
		}

		colCmd := exec.CommandContext(execCtx, "sqlite3", "-separator", "|", conn.Database, fmt.Sprintf("PRAGMA table_info(%s);", tableName))
		colOut, err := colCmd.CombinedOutput()
		if err == nil {
			colLines := strings.Split(strings.TrimSpace(string(colOut)), "\n")
			for _, cl := range colLines {
				cParts := strings.Split(cl, "|")
				if len(cParts) >= 6 {
					col := ColumnDetail{
						Name:         cParts[1],
						Type:         cParts[2],
						Nullable:     cParts[3] == "0",
						DefaultValue: cParts[4],
						PrimaryKey:   cParts[5] != "0",
					}
					tbl.Columns = append(tbl.Columns, col)
				}
			}
		}

		if !opts.Summary {
			fkCmd := exec.CommandContext(execCtx, "sqlite3", "-separator", "|", conn.Database, fmt.Sprintf("PRAGMA foreign_key_list(%s);", tableName))
			fkOut, err := fkCmd.CombinedOutput()
			if err == nil {
				fkLines := strings.Split(strings.TrimSpace(string(fkOut)), "\n")
				for _, fkl := range fkLines {
					fParts := strings.Split(fkl, "|")
					if len(fParts) >= 5 {
						tbl.ForeignKeys = append(tbl.ForeignKeys, ForeignKeyDetail{
							Column:           fParts[3],
							ReferencedTable:  fParts[2],
							ReferencedColumn: fParts[4],
						})
					}
				}
			}
		}

		result.Tables = append(result.Tables, tbl)
	}

	return result, nil
}

func introspectPostgres(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	if _, err := exec.LookPath("psql"); err != nil {
		return result, fmt.Errorf("psql CLI client is not installed in $PATH")
	}

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := "SELECT table_name, column_name, data_type, is_nullable, COALESCE(column_default, '') FROM information_schema.columns WHERE table_schema = 'public'"
	if opts.Filter != "" {
		if !isValidSQLIdentifier(opts.Filter) {
			return result, fmt.Errorf("invalid table filter")
		}
		query += fmt.Sprintf(" AND table_name LIKE '%%%s%%'", opts.Filter)
	}
	query += " ORDER BY table_name, ordinal_position;"

	args := []string{"-d", conn.Database, "-t", "-A", "-F", "|", "-c", query}
	if conn.Host != "" {
		args = append(args, "-h", conn.Host)
	}
	if conn.Port != "" {
		args = append(args, "-p", conn.Port)
	}
	if conn.Username != "" {
		args = append(args, "-U", conn.Username)
	}

	cmd := exec.CommandContext(execCtx, "psql", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("failed to introspect postgres: %v, out: %s", err, string(out))
	}

	tableMap := make(map[string]*TableDetail)
	var tableOrder []string

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		colName := strings.TrimSpace(parts[1])
		dataType := strings.TrimSpace(parts[2])
		isNullable := strings.ToUpper(strings.TrimSpace(parts[3])) == "YES"
		colDefault := strings.TrimSpace(parts[4])

		tbl, exists := tableMap[tableName]
		if !exists {
			tbl = &TableDetail{
				Name:    tableName,
				Type:    "TABLE",
				Columns: make([]ColumnDetail, 0),
			}
			tableMap[tableName] = tbl
			tableOrder = append(tableOrder, tableName)
		}

		tbl.Columns = append(tbl.Columns, ColumnDetail{
			Name:         colName,
			Type:         dataType,
			Nullable:     isNullable,
			DefaultValue: colDefault,
			PrimaryKey:   strings.Contains(strings.ToLower(colDefault), "nextval") || colName == "id",
		})
	}

	for _, name := range tableOrder {
		result.Tables = append(result.Tables, *tableMap[name])
	}

	return result, nil
}

func introspectMySQL(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	if _, err := exec.LookPath("mysql"); err != nil {
		return result, fmt.Errorf("mysql CLI client is not installed in $PATH")
	}

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dbName := conn.Database
	if !isValidSQLIdentifier(dbName) {
		return result, fmt.Errorf("invalid database name")
	}

	query := fmt.Sprintf("SELECT table_name, column_name, data_type, is_nullable, COALESCE(column_default, ''), column_key FROM information_schema.columns WHERE table_schema = '%s'", dbName)
	if opts.Filter != "" {
		if !isValidSQLIdentifier(opts.Filter) {
			return result, fmt.Errorf("invalid table filter")
		}
		query += fmt.Sprintf(" AND table_name LIKE '%%%s%%'", opts.Filter)
	}
	query += " ORDER BY table_name, ordinal_position;"

	args := []string{"-D", conn.Database, "-N", "-B", "-e", query}
	if conn.Host != "" {
		args = append(args, "-h", conn.Host)
	}
	if conn.Port != "" {
		args = append(args, "-P", conn.Port)
	}
	if conn.Username != "" {
		args = append(args, "-u", conn.Username)
	}

	cmd := exec.CommandContext(execCtx, "mysql", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("failed to introspect mysql: %v, out: %s", err, string(out))
	}

	tableMap := make(map[string]*TableDetail)
	var tableOrder []string

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		colName := strings.TrimSpace(parts[1])
		dataType := strings.TrimSpace(parts[2])
		isNullable := strings.ToUpper(strings.TrimSpace(parts[3])) == "YES"
		colDefault := strings.TrimSpace(parts[4])
		colKey := ""
		if len(parts) >= 6 {
			colKey = strings.TrimSpace(parts[5])
		}

		tbl, exists := tableMap[tableName]
		if !exists {
			tbl = &TableDetail{
				Name:    tableName,
				Type:    "TABLE",
				Columns: make([]ColumnDetail, 0),
			}
			tableMap[tableName] = tbl
			tableOrder = append(tableOrder, tableName)
		}

		tbl.Columns = append(tbl.Columns, ColumnDetail{
			Name:         colName,
			Type:         dataType,
			Nullable:     isNullable,
			DefaultValue: colDefault,
			PrimaryKey:   colKey == "PRI" || colName == "id",
		})
	}

	for _, name := range tableOrder {
		result.Tables = append(result.Tables, *tableMap[name])
	}

	return result, nil
}

func isValidSQLIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}
