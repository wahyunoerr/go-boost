package detector

import (
	"os/exec"
	"sort"
)

type ToolAvailability struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Path      string   `json:"path,omitempty"`
	NeededBy  []string `json:"needed_by"`
	Install   string   `json:"install,omitempty"`
}

type Capabilities struct {
	Tools            []ToolAvailability `json:"external_tools"`
	UnavailableTools []string           `json:"unavailable_go_boost_tools,omitempty"`
}

var externalTools = []ToolAvailability{
	{
		Name:     "sqlite3",
		NeededBy: []string{"db_query", "db_schema", "schema_struct_diff", "migration_generate"},
		Install:  "macOS: preinstalled or `brew install sqlite`. Debian/Ubuntu: `apt install sqlite3`. Windows: https://sqlite.org/download.html",
	},
	{
		Name:     "psql",
		NeededBy: []string{"db_query", "db_schema", "schema_struct_diff", "migration_generate"},
		Install:  "macOS: `brew install libpq` then link, or `brew install postgresql`. Debian/Ubuntu: `apt install postgresql-client`. Windows: https://www.postgresql.org/download/windows/",
	},
	{
		Name:     "mysql",
		NeededBy: []string{"db_query", "db_schema", "schema_struct_diff", "migration_generate"},
		Install:  "macOS: `brew install mysql-client`. Debian/Ubuntu: `apt install mysql-client`. Windows: https://dev.mysql.com/downloads/shell/",
	},
}

func DetectCapabilities() *Capabilities {
	caps := &Capabilities{Tools: make([]ToolAvailability, 0, len(externalTools))}

	anyDatabaseClient := false
	for _, tool := range externalTools {
		entry := tool
		if path, err := exec.LookPath(tool.Name); err == nil {
			entry.Available = true
			entry.Path = path
			entry.Install = ""
			anyDatabaseClient = true
		}
		caps.Tools = append(caps.Tools, entry)
	}

	if !anyDatabaseClient {
		blocked := map[string]bool{}
		for _, tool := range externalTools {
			for _, name := range tool.NeededBy {
				blocked[name] = true
			}
		}
		for name := range blocked {
			caps.UnavailableTools = append(caps.UnavailableTools, name)
		}
		sort.Strings(caps.UnavailableTools)
	}

	return caps
}

func DatabaseClientFor(driver string) (string, string, bool) {
	name := ""
	switch driver {
	case "sqlite":
		name = "sqlite3"
	case "postgres":
		name = "psql"
	case "mysql":
		name = "mysql"
	default:
		return "", "", false
	}

	for _, tool := range externalTools {
		if tool.Name != name {
			continue
		}
		if _, err := exec.LookPath(name); err == nil {
			return name, "", true
		}
		return name, tool.Install, false
	}
	return name, "", false
}
