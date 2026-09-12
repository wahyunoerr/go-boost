package database

import (
	"testing"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
)

func TestCompareTableAndStruct(t *testing.T) {
	tbl := TableDetail{
		Name: "users",
		Columns: []ColumnDetail{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "email", Type: "VARCHAR(255)"},
			{Name: "phone", Type: "VARCHAR(50)"},
		},
	}

	st := astparser.StructInfo{
		Name: "User",
		Fields: []astparser.FieldInfo{
			{Name: "ID", Type: "int", ParsedTags: map[string]string{"db": "id"}},
			{Name: "Email", Type: "string", ParsedTags: map[string]string{"db": "email"}},
		},
	}

	diff := compareTableAndStruct(tbl, st)
	if diff.IsSynchronized {
		t.Errorf("expected struct to be out of sync with table due to missing phone")
	}

	if len(diff.MissingInStruct) != 1 || diff.MissingInStruct[0] != "phone" {
		t.Errorf("expected 'phone' to be missing in struct, got %v", diff.MissingInStruct)
	}
}
