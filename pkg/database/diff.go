package database

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
)

type SchemaDiffResult struct {
	TableName         string   `json:"table_name"`
	MatchedStruct     string   `json:"matched_struct"`
	MissingInStruct   []string `json:"missing_in_struct,omitempty"`
	MissingInDatabase []string `json:"missing_in_database,omitempty"`
	Summary           string   `json:"summary"`
	IsSynchronized    bool     `json:"is_synchronized"`
}

func DiffSchemaAndStructs(ctx context.Context, conn *DBConnection, rootDir string) ([]SchemaDiffResult, error) {
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	schema, err := Introspect(ctx, conn, SchemaOptions{
		Summary:              false,
		IncludeColumnDetails: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to introspect database: %w", err)
	}

	symbols, err := astparser.ParsePath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Go AST: %w", err)
	}

	results := make([]SchemaDiffResult, 0)

	for _, tbl := range schema.Tables {
		var matchedStruct *astparser.StructInfo
		for i := range symbols.Structs {
			st := &symbols.Structs[i]

			if strings.EqualFold(st.Name, tbl.Name) ||
				strings.EqualFold(st.Name+"s", tbl.Name) ||
				strings.EqualFold(st.Name, strings.TrimSuffix(tbl.Name, "s")) {
				matchedStruct = st
				break
			}
		}

		if matchedStruct == nil {
			results = append(results, SchemaDiffResult{
				TableName:      tbl.Name,
				MatchedStruct:  "none",
				Summary:        fmt.Sprintf("Table '%s' has no matching Go struct in AST", tbl.Name),
				IsSynchronized: false,
			})
			continue
		}

		diff := compareTableAndStruct(tbl, *matchedStruct)
		results = append(results, diff)
	}

	return results, nil
}

func compareTableAndStruct(tbl TableDetail, st astparser.StructInfo) SchemaDiffResult {
	structFields := make(map[string]bool)
	for _, f := range st.Fields {
		if f.Embedded {
			continue
		}
		if isIgnoredColumnTag(f.ParsedTags) {
			continue
		}
		if dbTag, ok := f.ParsedTags["db"]; ok && dbTag != "" {
			structFields[strings.ToLower(dbTag)] = true
		} else if gormTag, ok := f.ParsedTags["gorm"]; ok && strings.Contains(gormTag, "column:") {
			parts := strings.Split(gormTag, ";")
			for _, p := range parts {
				if strings.HasPrefix(p, "column:") {
					col := strings.TrimPrefix(p, "column:")
					structFields[strings.ToLower(col)] = true
				}
			}
		} else if jsonTag, ok := f.ParsedTags["json"]; ok && jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			structFields[strings.ToLower(parts[0])] = true
		} else {
			structFields[strings.ToLower(f.Name)] = true
		}
	}

	dbColumns := make(map[string]bool)
	var missingInStruct []string
	for _, col := range tbl.Columns {
		colLower := strings.ToLower(col.Name)
		dbColumns[colLower] = true
		if !structFields[colLower] && !structFields[strings.ReplaceAll(colLower, "_", "")] {
			missingInStruct = append(missingInStruct, col.Name)
		}
	}

	var missingInDB []string
	for sf := range structFields {
		if sf == "-" || sf == "id" || sf == "created_at" || sf == "updated_at" {
			continue
		}
		if !dbColumns[sf] && !dbColumns[toSnakeCase(sf)] {
			missingInDB = append(missingInDB, sf)
		}
	}
	sort.Strings(missingInDB)

	isSync := len(missingInStruct) == 0 && len(missingInDB) == 0
	summary := "Table and struct schemas are fully synchronized"
	if !isSync {
		summary = fmt.Sprintf("Mismatches found: %d missing in struct, %d missing in DB", len(missingInStruct), len(missingInDB))
	}

	return SchemaDiffResult{
		TableName:         tbl.Name,
		MatchedStruct:     st.Name,
		MissingInStruct:   missingInStruct,
		MissingInDatabase: missingInDB,
		Summary:           summary,
		IsSynchronized:    isSync,
	}
}

func toSnakeCase(s string) string {
	var res strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			res.WriteRune('_')
		}
		res.WriteRune(r)
	}
	return strings.ToLower(res.String())
}

func isIgnoredColumnTag(tags map[string]string) bool {
	for _, key := range []string{"db", "gorm", "json"} {
		value, ok := tags[key]
		if !ok {
			continue
		}
		field, _, _ := strings.Cut(value, ",")
		field, _, _ = strings.Cut(field, ";")
		if strings.TrimSpace(field) == "-" {
			return true
		}
	}
	return false
}
