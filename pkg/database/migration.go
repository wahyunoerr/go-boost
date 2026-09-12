package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
)

type MigrationFiles struct {
	UpPath   string `json:"up_path"`
	DownPath string `json:"down_path"`
	UpSQL    string `json:"up_sql"`
	DownSQL  string `json:"down_sql"`
}

func GenerateMigrationScaffold(ctx context.Context, conn *DBConnection, rootDir string) (*MigrationFiles, error) {
	diffs, err := DiffSchemaAndStructs(ctx, conn, rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate schema diff: %w", err)
	}

	var upStmts []string
	var downStmts []string

	symbols, _ := astparser.ParsePath(rootDir)
	structMap := make(map[string]astparser.StructInfo)
	if symbols != nil {
		for _, s := range symbols.Structs {
			structMap[strings.ToLower(s.Name)] = s
		}
	}

	for _, d := range diffs {
		st := structMap[strings.ToLower(d.MatchedStruct)]
		fieldTypes := make(map[string]string)
		for _, f := range st.Fields {
			colName := strings.ToLower(f.Name)
			if dbTag, ok := f.ParsedTags["db"]; ok && dbTag != "" {
				colName = strings.ToLower(dbTag)
			}
			fieldTypes[colName] = f.Type
			fieldTypes[toSnakeCase(f.Name)] = f.Type
		}

		if len(d.MissingInDatabase) > 0 {
			for _, col := range d.MissingInDatabase {
				goType := fieldTypes[strings.ToLower(col)]
				colType := mapGoTypeToSQL(goType, col)

				upStmts = append(upStmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", d.TableName, col, colType))
				downStmts = append(downStmts, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", d.TableName, col))
			}
		}
	}

	if len(upStmts) == 0 {
		return nil, fmt.Errorf("no schema discrepancies found; database and Go structs are already in sync")
	}

	timestamp := time.Now().Format("20060102150405")
	migrationsDir := filepath.Join(rootDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		return nil, err
	}

	upSQL := "-- Migration Up\n" + strings.Join(upStmts, "\n") + "\n"
	downSQL := "-- Migration Down\n" + strings.Join(downStmts, "\n") + "\n"

	upPath := filepath.Join(migrationsDir, fmt.Sprintf("%s_sync_schema.up.sql", timestamp))
	downPath := filepath.Join(migrationsDir, fmt.Sprintf("%s_sync_schema.down.sql", timestamp))

	if err := os.WriteFile(upPath, []byte(upSQL), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(downPath, []byte(downSQL), 0644); err != nil {
		return nil, err
	}

	return &MigrationFiles{
		UpPath:   upPath,
		DownPath: downPath,
		UpSQL:    upSQL,
		DownSQL:  downSQL,
	}, nil
}

func mapGoTypeToSQL(goType, colName string) string {
	t := strings.ToLower(strings.TrimPrefix(goType, "*"))
	switch t {
	case "int", "int32", "uint", "uint32":
		return "INTEGER"
	case "int64", "uint64":
		return "BIGINT"
	case "bool":
		return "BOOLEAN DEFAULT FALSE"
	case "float32", "float64":
		return "DECIMAL(12,2)"
	case "time.time":
		return "TIMESTAMP WITH TIME ZONE"
	case "[]byte":
		return "BYTEA"
	case "uuid.uuid":
		return "UUID"
	default:
		if strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "map[") {
			return "JSONB"
		}
		if strings.Contains(strings.ToLower(colName), "id") || strings.Contains(strings.ToLower(colName), "count") {
			return "BIGINT"
		}
		if strings.Contains(strings.ToLower(colName), "is_") || strings.Contains(strings.ToLower(colName), "has_") {
			return "BOOLEAN DEFAULT FALSE"
		}
		return "VARCHAR(255)"
	}
}
