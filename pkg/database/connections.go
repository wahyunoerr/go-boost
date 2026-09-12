package database

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type DBConnection struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	Database string `json:"database"`
	Username string `json:"username,omitempty"`
	DSN      string `json:"dsn,omitempty"`
	Source   string `json:"source"`
}

type DiscoveredConnections struct {
	DefaultConnection string         `json:"default_connection"`
	Connections       []DBConnection `json:"connections"`
}

func DiscoverConnections(rootDir string) (*DiscoveredConnections, error) {
	result := &DiscoveredConnections{
		Connections: make([]DBConnection, 0),
	}

	seen := make(map[string]bool)
	addConn := func(c DBConnection) {
		key := fmt.Sprintf("%s::%s::%s::%s", c.Driver, c.Host, c.Port, c.Database)
		if !seen[key] {
			seen[key] = true
			result.Connections = append(result.Connections, c)
		}
	}

	osVars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			osVars[parts[0]] = parts[1]
		}
	}
	for _, c := range parseVarsMap(osVars, "os_environ") {
		addConn(c)
	}

	envFiles := []string{".env", ".env.local", ".env.development", ".env.example"}
	for _, envName := range envFiles {
		p := filepath.Join(rootDir, envName)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			conns := parseEnvFile(p)
			for _, c := range conns {
				addConn(c)
			}
		}
	}

	_ = filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			if fi != nil && (strings.HasPrefix(fi.Name(), ".") || fi.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
			rel, _ := filepath.Rel(rootDir, path)
			addConn(DBConnection{
				Name:     filepath.Base(path),
				Driver:   "sqlite",
				Database: rel,
				Source:   rel,
			})
		}
		return nil
	})

	if len(result.Connections) > 0 {
		result.DefaultConnection = result.Connections[0].Name
	}

	return result, nil
}

func parseEnvFile(path string) []DBConnection {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			vars[k] = v
		}
	}

	return parseVarsMap(vars, filepath.Base(path))
}

func parseVarsMap(vars map[string]string, source string) []DBConnection {
	var conns []DBConnection

	urlKeys := []string{"DATABASE_URL", "POSTGRES_URL", "MYSQL_URL", "SQLITE_URL"}
	for _, k := range urlKeys {
		if rawURL, ok := vars[k]; ok && rawURL != "" {
			if parsed, ok := parseDSN(rawURL, source); ok {
				conns = append(conns, parsed)
			}
		}
	}

	driver := vars["DB_CONNECTION"]
	if driver == "" {
		driver = vars["DB_DRIVER"]
	}
	dbName := vars["DB_DATABASE"]
	if dbName == "" {
		dbName = vars["DB_NAME"]
	}

	if driver != "" || dbName != "" {
		if driver == "" {
			driver = "postgres"
		}
		conns = append(conns, DBConnection{
			Name:     "app_db",
			Driver:   normalizeDriver(driver),
			Host:     vars["DB_HOST"],
			Port:     vars["DB_PORT"],
			Database: dbName,
			Username: vars["DB_USERNAME"],
			Source:   source,
		})
	}

	return conns
}

func parseDSN(dsn, source string) (DBConnection, bool) {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		driver := normalizeDriver(u.Scheme)
		db := strings.TrimPrefix(u.Path, "/")
		username := ""
		if u.User != nil {
			username = u.User.Username()
		}
		return DBConnection{
			Name:     driver + "_conn",
			Driver:   driver,
			Host:     u.Hostname(),
			Port:     u.Port(),
			Database: db,
			Username: username,
			DSN:      dsn,
			Source:   source,
		}, true
	}

	if strings.Contains(dsn, "host=") && strings.Contains(dsn, "dbname=") {
		conn := DBConnection{
			Name:   "postgres_kv",
			Driver: "postgres",
			DSN:    dsn,
			Source: source,
		}
		parts := strings.Fields(dsn)
		for _, p := range parts {
			kv := strings.SplitN(p, "=", 2)
			if len(kv) == 2 {
				switch kv[0] {
				case "host":
					conn.Host = kv[1]
				case "port":
					conn.Port = kv[1]
				case "dbname":
					conn.Database = kv[1]
				case "user":
					conn.Username = kv[1]
				}
			}
		}
		return conn, true
	}

	return DBConnection{}, false
}

func normalizeDriver(driver string) string {
	d := strings.ToLower(driver)
	switch {
	case strings.Contains(d, "postg"), d == "pg", d == "pq":
		return "postgres"
	case strings.Contains(d, "my"), strings.Contains(d, "maria"):
		return "mysql"
	case strings.Contains(d, "sqlite"):
		return "sqlite"
	default:
		return d
	}
}

func ToJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
