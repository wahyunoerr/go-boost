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
	Password string `json:"-"`
	DSN      string `json:"dsn,omitempty"`
	RawDSN   string `json:"-"`
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

	envFiles := []string{".env", ".env.local", ".env.development", ".env.example"}
	for _, envName := range envFiles {
		p := filepath.Join(rootDir, envName)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			for _, c := range parseEnvFile(p) {
				addConn(c)
			}
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

	_ = filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			if path != rootDir && shouldSkipWalkDir(fi.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
			rel, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				rel = path
			}
			addConn(DBConnection{
				Name:     filepath.Base(path),
				Driver:   "sqlite",
				Database: filepath.Join(rootDir, rel),
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

func shouldSkipWalkDir(name string) bool {
	return strings.HasPrefix(name, ".") ||
		name == "vendor" ||
		name == "node_modules" ||
		name == "third_party" ||
		name == "testdata"
}

func parseEnvFile(path string) []DBConnection {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
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

	urlKeys := []string{"DATABASE_URL", "POSTGRES_URL", "MYSQL_URL", "SQLITE_URL", "DB_DSN", "DB_URL"}
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
		password := vars["DB_PASSWORD"]
		if password == "" {
			password = vars["DB_PASS"]
		}
		conns = append(conns, DBConnection{
			Name:     "app_db",
			Driver:   normalizeDriver(driver),
			Host:     vars["DB_HOST"],
			Port:     vars["DB_PORT"],
			Database: dbName,
			Username: vars["DB_USERNAME"],
			Password: password,
			Source:   source,
		})
	}

	return conns
}

func parseDSN(dsn, source string) (DBConnection, bool) {
	if conn, ok := parseGoSQLDSN(dsn, source); ok {
		return conn, true
	}

	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" && strings.Contains(dsn, "://") {
		driver := normalizeDriver(u.Scheme)
		db := strings.TrimPrefix(u.Path, "/")
		username := ""
		password := ""
		if u.User != nil {
			username = u.User.Username()
			password, _ = u.User.Password()
		}
		if driver == "sqlite" && db == "" {
			db = u.Opaque
		}
		return DBConnection{
			Name:     driver + "_conn",
			Driver:   driver,
			Host:     u.Hostname(),
			Port:     u.Port(),
			Database: db,
			Username: username,
			Password: password,
			DSN:      redactDSN(dsn),
			RawDSN:   dsn,
			Source:   source,
		}, true
	}

	if strings.Contains(dsn, "host=") && strings.Contains(dsn, "dbname=") {
		conn := DBConnection{
			Name:   "postgres_kv",
			Driver: "postgres",
			DSN:    redactDSN(dsn),
			RawDSN: dsn,
			Source: source,
		}
		for _, p := range strings.Fields(dsn) {
			kv := strings.SplitN(p, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "host":
				conn.Host = kv[1]
			case "port":
				conn.Port = kv[1]
			case "dbname":
				conn.Database = kv[1]
			case "user":
				conn.Username = kv[1]
			case "password":
				conn.Password = kv[1]
			}
		}
		return conn, true
	}

	return DBConnection{}, false
}

func parseGoSQLDSN(dsn, source string) (DBConnection, bool) {
	if strings.Contains(dsn, "://") {
		return DBConnection{}, false
	}

	at := strings.LastIndex(dsn, "@")
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 || slash < at {
		return DBConnection{}, false
	}

	credentials := ""
	remainder := dsn
	if at >= 0 {
		credentials = dsn[:at]
		remainder = dsn[at+1:]
		slash = strings.LastIndex(remainder, "/")
		if slash < 0 {
			return DBConnection{}, false
		}
	}

	address := remainder[:slash]
	database := remainder[slash+1:]
	if idx := strings.IndexAny(database, "?"); idx >= 0 {
		database = database[:idx]
	}

	open := strings.Index(address, "(")
	if open < 0 && credentials == "" {
		return DBConnection{}, false
	}

	host, port := "", ""
	if open >= 0 && strings.HasSuffix(address, ")") {
		hostPort := address[open+1 : len(address)-1]
		host, port = splitHostPort(hostPort)
	} else if address != "" {
		host, port = splitHostPort(address)
	}

	username, password := credentials, ""
	if idx := strings.Index(credentials, ":"); idx >= 0 {
		username = credentials[:idx]
		password = credentials[idx+1:]
	}

	return DBConnection{
		Name:     "mysql_conn",
		Driver:   "mysql",
		Host:     host,
		Port:     port,
		Database: database,
		Username: username,
		Password: password,
		DSN:      redactDSN(dsn),
		RawDSN:   dsn,
		Source:   source,
	}, true
}

func splitHostPort(hostPort string) (string, string) {
	idx := strings.LastIndex(hostPort, ":")
	if idx < 0 {
		return hostPort, ""
	}
	return hostPort[:idx], hostPort[idx+1:]
}

func redactDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && strings.Contains(dsn, "://") && u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "***")
			return u.String()
		}
		return dsn
	}

	if strings.Contains(dsn, "password=") {
		fields := strings.Fields(dsn)
		for i, f := range fields {
			if strings.HasPrefix(f, "password=") {
				fields[i] = "password=***"
			}
		}
		return strings.Join(fields, " ")
	}

	if at := strings.LastIndex(dsn, "@"); at > 0 {
		credentials := dsn[:at]
		if colon := strings.Index(credentials, ":"); colon >= 0 {
			return credentials[:colon] + ":***" + dsn[at:]
		}
	}

	return dsn
}

func normalizeDriver(driver string) string {
	d := strings.ToLower(strings.TrimSpace(driver))
	switch {
	case strings.HasPrefix(d, "postgres"), d == "pg", d == "pq", d == "pgx", d == "psql":
		return "postgres"
	case strings.HasPrefix(d, "mysql"), strings.HasPrefix(d, "maria"):
		return "mysql"
	case strings.HasPrefix(d, "sqlite"), d == "file":
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
