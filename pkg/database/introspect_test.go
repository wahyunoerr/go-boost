package database

import (
	"strings"
	"testing"
)

func TestQuoteSQLIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "users", `"users"`},
		{"with space", "user data", `"user data"`},
		{"with hyphen", "user-data", `"user-data"`},
		{"embedded quote is doubled", `we"ird`, `"we""ird"`},
		{"injection attempt is neutralised", `users"; DROP TABLE x; --`, `"users""; DROP TABLE x; --"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := quoteSQLIdentifier(tc.input); got != tc.want {
				t.Errorf("quoteSQLIdentifier(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestQuoteSQLString(t *testing.T) {
	if got := quoteSQLString("O'Brien"); got != "'O''Brien'" {
		t.Errorf("quoteSQLString = %q, want 'O''Brien'", got)
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name   string
		table  string
		filter string
		want   bool
	}{
		{"empty filter matches everything", "users", "", true},
		{"substring matches", "app_users", "user", true},
		{"case insensitive", "Users", "user", true},
		{"no match", "orders", "user", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesFilter(tc.table, tc.filter); got != tc.want {
				t.Errorf("matchesFilter(%q,%q) = %v, want %v", tc.table, tc.filter, got, tc.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines("  \n\n  "); got != nil {
		t.Errorf("expected nil for blank input, got %#v", got)
	}
	got := splitLines("a\nb\nc\n")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("splitLines = %#v", got)
	}
}

func TestMarkPrimaryKey(t *testing.T) {
	tbl := &TableDetail{Columns: []ColumnDetail{{Name: "id"}, {Name: "email"}}}
	markPrimaryKey(tbl, "email")

	if tbl.Columns[0].PrimaryKey {
		t.Error("id was marked even though the constraint named email")
	}
	if !tbl.Columns[1].PrimaryKey {
		t.Error("email should have been marked as the primary key")
	}

	markPrimaryKey(tbl, "missing")
}

func TestAddIndexColumnGroupsByIndexName(t *testing.T) {
	tbl := &TableDetail{}
	addIndexColumn(tbl, "idx_email", true, "email")
	addIndexColumn(tbl, "idx_name", false, "first_name")
	addIndexColumn(tbl, "idx_name", false, "last_name")

	if len(tbl.Indexes) != 2 {
		t.Fatalf("expected 2 indexes, got %d", len(tbl.Indexes))
	}
	for _, idx := range tbl.Indexes {
		switch idx.Name {
		case "idx_email":
			if !idx.Unique || len(idx.Columns) != 1 {
				t.Errorf("idx_email = %+v", idx)
			}
		case "idx_name":
			if idx.Unique || len(idx.Columns) != 2 {
				t.Errorf("idx_name should be a two column non-unique index, got %+v", idx)
			}
		}
	}
}

func TestIntrospectRejectsUnknownDriver(t *testing.T) {
	_, err := Introspect(t.Context(), &DBConnection{Driver: "oracle", Name: "x"}, SchemaOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported driver") {
		t.Errorf("expected an unsupported driver error, got %v", err)
	}
}

func TestIntrospectRequiresAConnection(t *testing.T) {
	if _, err := Introspect(t.Context(), nil, SchemaOptions{}); err == nil {
		t.Error("expected an error when no connection is given")
	}
}

func TestRequireClientExplainsHowToInstall(t *testing.T) {
	err := requireClient("postgres")
	if err == nil {
		t.Skip("psql is installed on this machine")
	}
	for _, want := range []string{"psql", "Install it with", "app_info"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message should mention %q, got: %v", want, err)
		}
	}
}

func TestRequireClientUnknownDriver(t *testing.T) {
	if err := requireClient("oracle"); err == nil {
		t.Error("expected an error for an unknown driver")
	}
}
