package database

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const introspectTimeout = 30 * time.Second

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

	ctx, cancel := context.WithTimeout(ctx, introspectTimeout)
	defer cancel()

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

func matchesFilter(name, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteSQLString(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func sqliteCmd(ctx context.Context, conn *DBConnection, query string) *exec.Cmd {
	return exec.CommandContext(ctx, "sqlite3", "-readonly", "-separator", "|", safeArgValue(conn.Database), query)
}

func introspectSQLite(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	if err := requireClient("sqlite"); err != nil {
		return result, err
	}

	query := "SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%';"
	out, err := sqliteCmd(ctx, conn, query).CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("failed to introspect sqlite: %v, out: %s", err, string(out))
	}

	for _, l := range splitLines(string(out)) {
		parts := strings.Split(l, "|")
		if len(parts) < 2 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		tableType := strings.TrimSpace(parts[1])

		if !opts.IncludeViews && tableType == "view" {
			continue
		}
		if !matchesFilter(tableName, opts.Filter) {
			continue
		}

		tbl := TableDetail{
			Name:    tableName,
			Type:    strings.ToUpper(tableType),
			Columns: make([]ColumnDetail, 0),
		}

		quoted := quoteSQLIdentifier(tableName)

		colOut, err := sqliteCmd(ctx, conn, fmt.Sprintf("PRAGMA table_info(%s);", quoted)).CombinedOutput()
		if err == nil {
			for _, cl := range splitLines(string(colOut)) {
				cParts := strings.Split(cl, "|")
				if len(cParts) < 6 {
					continue
				}
				tbl.Columns = append(tbl.Columns, ColumnDetail{
					Name:         cParts[1],
					Type:         cParts[2],
					Nullable:     cParts[3] == "0",
					DefaultValue: cParts[4],
					PrimaryKey:   cParts[5] != "0",
				})
			}
		}

		if !opts.Summary {
			fkOut, err := sqliteCmd(ctx, conn, fmt.Sprintf("PRAGMA foreign_key_list(%s);", quoted)).CombinedOutput()
			if err == nil {
				for _, fkl := range splitLines(string(fkOut)) {
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

			idxOut, err := sqliteCmd(ctx, conn, fmt.Sprintf("PRAGMA index_list(%s);", quoted)).CombinedOutput()
			if err == nil {
				for _, il := range splitLines(string(idxOut)) {
					iParts := strings.Split(il, "|")
					if len(iParts) < 3 {
						continue
					}
					idx := IndexDetail{Name: iParts[1], Unique: iParts[2] == "1"}

					colOut, err := sqliteCmd(ctx, conn, fmt.Sprintf("PRAGMA index_info(%s);", quoteSQLIdentifier(idx.Name))).CombinedOutput()
					if err == nil {
						for _, cl := range splitLines(string(colOut)) {
							cParts := strings.Split(cl, "|")
							if len(cParts) >= 3 && cParts[2] != "" {
								idx.Columns = append(idx.Columns, cParts[2])
							}
						}
					}
					tbl.Indexes = append(tbl.Indexes, idx)
				}
			}
		}

		result.Tables = append(result.Tables, tbl)
	}

	return result, nil
}

func runPsqlQuery(ctx context.Context, conn *DBConnection, query string) ([]string, error) {
	args := []string{"-v", "ON_ERROR_STOP=1", "-d", safeArgValue(conn.Database), "-t", "-A", "-F", "|", "-c", query}
	args = appendPostgresConnArgs(args, conn)

	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = postgresEnv(conn)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v, out: %s", err, truncateOutput(string(out)))
	}
	return splitLines(string(out)), nil
}

func introspectPostgres(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	if err := requireClient("postgres"); err != nil {
		return result, err
	}

	tableTypes := "'BASE TABLE'"
	if opts.IncludeViews {
		tableTypes = "'BASE TABLE', 'VIEW'"
	}

	columnQuery := fmt.Sprintf(`SELECT c.table_name, c.column_name, c.data_type, c.is_nullable,
COALESCE(c.column_default, ''), COALESCE(c.is_identity, 'NO'), t.table_type
FROM information_schema.columns c
JOIN information_schema.tables t
  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = 'public' AND t.table_type IN (%s)
ORDER BY c.table_name, c.ordinal_position;`, tableTypes)

	lines, err := runPsqlQuery(ctx, conn, columnQuery)
	if err != nil {
		return result, fmt.Errorf("failed to introspect postgres: %w", err)
	}

	tableMap := make(map[string]*TableDetail)
	var tableOrder []string

	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 7 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		if !matchesFilter(tableName, opts.Filter) {
			continue
		}

		tbl, exists := tableMap[tableName]
		if !exists {
			tableType := "TABLE"
			if strings.EqualFold(strings.TrimSpace(parts[6]), "VIEW") {
				tableType = "VIEW"
			}
			tbl = &TableDetail{Name: tableName, Type: tableType, Columns: make([]ColumnDetail, 0)}
			tableMap[tableName] = tbl
			tableOrder = append(tableOrder, tableName)
		}

		colDefault := strings.TrimSpace(parts[4])
		tbl.Columns = append(tbl.Columns, ColumnDetail{
			Name:          strings.TrimSpace(parts[1]),
			Type:          strings.TrimSpace(parts[2]),
			Nullable:      strings.EqualFold(strings.TrimSpace(parts[3]), "YES"),
			DefaultValue:  colDefault,
			AutoIncrement: strings.Contains(strings.ToLower(colDefault), "nextval") || strings.EqualFold(strings.TrimSpace(parts[5]), "YES"),
		})
	}

	pkQuery := `SELECT tc.table_name, kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
WHERE tc.table_schema = 'public' AND tc.constraint_type = 'PRIMARY KEY';`

	if pkLines, err := runPsqlQuery(ctx, conn, pkQuery); err == nil {
		for _, line := range pkLines {
			parts := strings.Split(line, "|")
			if len(parts) < 2 {
				continue
			}
			if tbl, ok := tableMap[strings.TrimSpace(parts[0])]; ok {
				markPrimaryKey(tbl, strings.TrimSpace(parts[1]))
			}
		}
	}

	if !opts.Summary {
		fkQuery := `SELECT tc.table_name, kcu.column_name, ccu.table_name, ccu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
WHERE tc.table_schema = 'public' AND tc.constraint_type = 'FOREIGN KEY';`

		if fkLines, err := runPsqlQuery(ctx, conn, fkQuery); err == nil {
			for _, line := range fkLines {
				parts := strings.Split(line, "|")
				if len(parts) < 4 {
					continue
				}
				if tbl, ok := tableMap[strings.TrimSpace(parts[0])]; ok {
					tbl.ForeignKeys = append(tbl.ForeignKeys, ForeignKeyDetail{
						Column:           strings.TrimSpace(parts[1]),
						ReferencedTable:  strings.TrimSpace(parts[2]),
						ReferencedColumn: strings.TrimSpace(parts[3]),
					})
				}
			}
		}

		idxQuery := `SELECT t.relname, i.relname, ix.indisunique, a.attname
FROM pg_class t
JOIN pg_index ix ON t.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = 'public'
ORDER BY t.relname, i.relname;`

		if idxLines, err := runPsqlQuery(ctx, conn, idxQuery); err == nil {
			for _, line := range idxLines {
				parts := strings.Split(line, "|")
				if len(parts) < 4 {
					continue
				}
				if tbl, ok := tableMap[strings.TrimSpace(parts[0])]; ok {
					addIndexColumn(tbl, strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]) == "t", strings.TrimSpace(parts[3]))
				}
			}
		}
	}

	for _, name := range tableOrder {
		result.Tables = append(result.Tables, *tableMap[name])
	}

	return result, nil
}

func runMySQLQuery(ctx context.Context, conn *DBConnection, query string) ([]string, error) {
	args := []string{
		"--init-command=SET SESSION TRANSACTION READ ONLY",
		"-D", safeArgValue(conn.Database),
		"-N", "-B", "-e", query,
	}
	args = appendMySQLConnArgs(args, conn)

	cmd := exec.CommandContext(ctx, "mysql", args...)
	cmd.Env = mysqlEnv(conn)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v, out: %s", err, truncateOutput(string(out)))
	}
	return splitLines(string(out)), nil
}

func introspectMySQL(ctx context.Context, conn *DBConnection, opts SchemaOptions, result *SchemaResult) (*SchemaResult, error) {
	if err := requireClient("mysql"); err != nil {
		return result, err
	}
	if conn.Database == "" {
		return result, fmt.Errorf("mysql connection has no database name")
	}

	schema := quoteSQLString(conn.Database)

	columnQuery := fmt.Sprintf(`SELECT c.table_name, c.column_name, c.data_type, c.is_nullable,
COALESCE(c.column_default, ''), c.column_key, c.extra, t.table_type
FROM information_schema.columns c
JOIN information_schema.tables t
  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = %s
ORDER BY c.table_name, c.ordinal_position;`, schema)

	lines, err := runMySQLQuery(ctx, conn, columnQuery)
	if err != nil {
		return result, fmt.Errorf("failed to introspect mysql: %w", err)
	}

	tableMap := make(map[string]*TableDetail)
	var tableOrder []string

	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 8 {
			continue
		}
		tableName := strings.TrimSpace(parts[0])
		if !matchesFilter(tableName, opts.Filter) {
			continue
		}

		isView := strings.EqualFold(strings.TrimSpace(parts[7]), "VIEW")
		if isView && !opts.IncludeViews {
			continue
		}

		tbl, exists := tableMap[tableName]
		if !exists {
			tableType := "TABLE"
			if isView {
				tableType = "VIEW"
			}
			tbl = &TableDetail{Name: tableName, Type: tableType, Columns: make([]ColumnDetail, 0)}
			tableMap[tableName] = tbl
			tableOrder = append(tableOrder, tableName)
		}

		tbl.Columns = append(tbl.Columns, ColumnDetail{
			Name:          strings.TrimSpace(parts[1]),
			Type:          strings.TrimSpace(parts[2]),
			Nullable:      strings.EqualFold(strings.TrimSpace(parts[3]), "YES"),
			DefaultValue:  strings.TrimSpace(parts[4]),
			PrimaryKey:    strings.TrimSpace(parts[5]) == "PRI",
			AutoIncrement: strings.Contains(strings.ToLower(parts[6]), "auto_increment"),
		})
	}

	if !opts.Summary {
		fkQuery := fmt.Sprintf(`SELECT table_name, column_name, referenced_table_name, referenced_column_name
FROM information_schema.key_column_usage
WHERE table_schema = %s AND referenced_table_name IS NOT NULL;`, schema)

		if fkLines, err := runMySQLQuery(ctx, conn, fkQuery); err == nil {
			for _, line := range fkLines {
				parts := strings.Split(line, "\t")
				if len(parts) < 4 {
					continue
				}
				if tbl, ok := tableMap[strings.TrimSpace(parts[0])]; ok {
					tbl.ForeignKeys = append(tbl.ForeignKeys, ForeignKeyDetail{
						Column:           strings.TrimSpace(parts[1]),
						ReferencedTable:  strings.TrimSpace(parts[2]),
						ReferencedColumn: strings.TrimSpace(parts[3]),
					})
				}
			}
		}

		idxQuery := fmt.Sprintf(`SELECT table_name, index_name, non_unique, column_name
FROM information_schema.statistics
WHERE table_schema = %s
ORDER BY table_name, index_name, seq_in_index;`, schema)

		if idxLines, err := runMySQLQuery(ctx, conn, idxQuery); err == nil {
			for _, line := range idxLines {
				parts := strings.Split(line, "\t")
				if len(parts) < 4 {
					continue
				}
				if tbl, ok := tableMap[strings.TrimSpace(parts[0])]; ok {
					addIndexColumn(tbl, strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]) == "0", strings.TrimSpace(parts[3]))
				}
			}
		}
	}

	for _, name := range tableOrder {
		result.Tables = append(result.Tables, *tableMap[name])
	}

	return result, nil
}

func markPrimaryKey(tbl *TableDetail, column string) {
	for i := range tbl.Columns {
		if tbl.Columns[i].Name == column {
			tbl.Columns[i].PrimaryKey = true
			return
		}
	}
}

func addIndexColumn(tbl *TableDetail, indexName string, unique bool, column string) {
	for i := range tbl.Indexes {
		if tbl.Indexes[i].Name == indexName {
			tbl.Indexes[i].Columns = append(tbl.Indexes[i].Columns, column)
			return
		}
	}
	tbl.Indexes = append(tbl.Indexes, IndexDetail{
		Name:    indexName,
		Unique:  unique,
		Columns: []string{column},
	})
}

func splitLines(out string) []string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
