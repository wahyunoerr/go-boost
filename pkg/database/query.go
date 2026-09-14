package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	queryTimeout   = 10 * time.Second
	maxQueryOutput = 32 * 1024
	defaultRowCap  = 100
)

var (
	allowedLeadingVerbs = regexp.MustCompile(`(?i)^(SELECT|WITH|SHOW|EXPLAIN|DESCRIBE|DESC|PRAGMA)\b`)

	forbiddenKeywords = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|RENAME|REPLACE|GRANT|REVOKE|EXEC|EXECUTE|ATTACH|DETACH|VACUUM|REINDEX|COPY|CALL|MERGE|UPSERT|LOAD|OUTFILE|DUMPFILE|INTO|SET|COMMIT|ROLLBACK|SAVEPOINT|LOCK|UNLOCK|PREPARE|DEALLOCATE|HANDLER|NOTIFY|LISTEN|CLUSTER|REFRESH|IMPORT|INSTALL)\b`)

	forbiddenFunctions = regexp.MustCompile(`(?i)\b(load_extension|readfile|writefile|edit|setval|nextval|pg_read_file|pg_read_binary_file|pg_ls_dir|pg_stat_file|pg_terminate_backend|pg_cancel_backend|pg_reload_conf|pg_rotate_logfile|pg_sleep|lo_import|lo_export|lo_unlink|dblink|dblink_exec|set_config|sleep|benchmark|sys_exec|sys_eval|randomblob)\s*\(`)

	limitKeyword = regexp.MustCompile(`(?i)\bLIMIT\b`)
	pragmaWrite  = regexp.MustCompile(`(?i)^PRAGMA\b[^=]*=`)
)

func ValidateReadOnlyQuery(rawQuery string) (string, error) {
	cleaned := strings.TrimSpace(stripSQLComments(rawQuery))
	if cleaned == "" {
		return "", fmt.Errorf("empty query provided")
	}

	statements := splitStatementsSafe(cleaned)
	if len(statements) == 0 {
		return "", fmt.Errorf("empty query provided")
	}
	if len(statements) > 1 {
		return "", fmt.Errorf("multiple queries are not allowed for safety")
	}

	singleStmt := strings.TrimSpace(statements[0])

	if !allowedLeadingVerbs.MatchString(singleStmt) {
		return "", fmt.Errorf("only read-only statements (SELECT, WITH, SHOW, EXPLAIN, DESCRIBE, PRAGMA) are allowed")
	}

	codeWithoutLiterals := blankStringLiterals(singleStmt)

	if pragmaWrite.MatchString(codeWithoutLiterals) {
		return "", fmt.Errorf("PRAGMA assignments modify the database and are not allowed")
	}
	if match := forbiddenKeywords.FindString(codeWithoutLiterals); match != "" {
		return "", fmt.Errorf("query contains forbidden keyword %q", strings.ToUpper(match))
	}
	if match := forbiddenFunctions.FindString(codeWithoutLiterals); match != "" {
		name := strings.TrimRight(strings.TrimSpace(match), "(")
		return "", fmt.Errorf("query calls forbidden function %q", strings.TrimSpace(name))
	}

	return singleStmt, nil
}

func stripSQLComments(query string) string {
	var out strings.Builder
	runes := []rune(query)

	inSingle, inDouble, inBacktick := false, false, false

	for i := 0; i < len(runes); i++ {
		c := runes[i]

		if inSingle {
			out.WriteRune(c)
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					out.WriteRune(runes[i+1])
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			out.WriteRune(c)
			if c == '"' {
				if i+1 < len(runes) && runes[i+1] == '"' {
					out.WriteRune(runes[i+1])
					i++
					continue
				}
				inDouble = false
			}
			continue
		}
		if inBacktick {
			out.WriteRune(c)
			if c == '`' {
				inBacktick = false
			}
			continue
		}

		switch c {
		case '\'':
			inSingle = true
			out.WriteRune(c)
			continue
		case '"':
			inDouble = true
			out.WriteRune(c)
			continue
		case '`':
			inBacktick = true
			out.WriteRune(c)
			continue
		case '-':
			if i+1 < len(runes) && runes[i+1] == '-' {
				for i < len(runes) && runes[i] != '\n' {
					i++
				}
				out.WriteRune(' ')
				continue
			}
		case '/':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i += 2
				for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
					i++
				}
				i++
				out.WriteRune(' ')
				continue
			}
		}

		out.WriteRune(c)
	}

	return out.String()
}

func blankStringLiterals(query string) string {
	var out strings.Builder
	runes := []rune(query)

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if c != '\'' && c != '"' {
			out.WriteRune(c)
			continue
		}

		quote := c
		out.WriteRune(quote)
		i++
		for i < len(runes) {
			if runes[i] == quote {
				if i+1 < len(runes) && runes[i+1] == quote {
					i += 2
					continue
				}
				break
			}
			i++
		}
		out.WriteRune(quote)
	}

	return out.String()
}

func splitStatementsSafe(query string) []string {
	var stmts []string
	var current strings.Builder

	inSingle, inDouble, inBacktick := false, false, false
	runes := []rune(query)

	for i := 0; i < len(runes); i++ {
		c := runes[i]

		switch {
		case inSingle:
			current.WriteRune(c)
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					current.WriteRune(runes[i+1])
					i++
					continue
				}
				inSingle = false
			}
			continue
		case inDouble:
			current.WriteRune(c)
			if c == '"' {
				if i+1 < len(runes) && runes[i+1] == '"' {
					current.WriteRune(runes[i+1])
					i++
					continue
				}
				inDouble = false
			}
			continue
		case inBacktick:
			current.WriteRune(c)
			if c == '`' {
				inBacktick = false
			}
			continue
		}

		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '`':
			inBacktick = true
		case ';':
			if s := strings.TrimSpace(current.String()); s != "" {
				stmts = append(stmts, s)
			}
			current.Reset()
			continue
		}

		current.WriteRune(c)
	}

	if s := strings.TrimSpace(current.String()); s != "" {
		stmts = append(stmts, s)
	}

	return stmts
}

func ExecuteQuery(ctx context.Context, conn *DBConnection, rawQuery string) (string, error) {
	safeQuery, err := ValidateReadOnlyQuery(rawQuery)
	if err != nil {
		return "", err
	}

	if conn == nil {
		return "", fmt.Errorf("no database connection configured; run db_connections to see what go-boost discovered")
	}

	upper := strings.ToUpper(safeQuery)
	if strings.HasPrefix(upper, "SELECT") && !limitKeyword.MatchString(blankStringLiterals(safeQuery)) {
		safeQuery = fmt.Sprintf("%s LIMIT %d", safeQuery, defaultRowCap)
	}

	execCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	switch conn.Driver {
	case "sqlite":
		return runSQLite(execCtx, conn, safeQuery)
	case "postgres":
		return runPostgres(execCtx, conn, safeQuery)
	case "mysql":
		return runMySQL(execCtx, conn, safeQuery)
	default:
		return "", fmt.Errorf("unsupported driver %q for query execution", conn.Driver)
	}
}

func runSQLite(ctx context.Context, conn *DBConnection, query string) (string, error) {
	if conn.Database == "" {
		return "", fmt.Errorf("sqlite connection has no database file")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return "", fmt.Errorf("sqlite3 CLI is not installed in $PATH")
	}

	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-header", "-table", safeArgValue(conn.Database), query)
	return runQueryCommand(ctx, cmd, "sqlite")
}

func runPostgres(ctx context.Context, conn *DBConnection, query string) (string, error) {
	if _, err := exec.LookPath("psql"); err != nil {
		return "", fmt.Errorf("psql CLI client is not installed in $PATH to execute PostgreSQL queries directly")
	}

	args := []string{"-v", "ON_ERROR_STOP=1", "-d", safeArgValue(conn.Database), "-c", query}
	args = appendPostgresConnArgs(args, conn)

	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = postgresEnv(conn)
	return runQueryCommand(ctx, cmd, "psql")
}

func runMySQL(ctx context.Context, conn *DBConnection, query string) (string, error) {
	if _, err := exec.LookPath("mysql"); err != nil {
		return "", fmt.Errorf("mysql CLI client is not installed in $PATH to execute MySQL queries directly")
	}

	args := []string{
		"--init-command=SET SESSION TRANSACTION READ ONLY",
		"-D", safeArgValue(conn.Database),
		"-e", query,
	}
	args = appendMySQLConnArgs(args, conn)

	cmd := exec.CommandContext(ctx, "mysql", args...)
	cmd.Env = mysqlEnv(conn)
	return runQueryCommand(ctx, cmd, "mysql")
}

func runQueryCommand(ctx context.Context, cmd *exec.Cmd, label string) (string, error) {
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%s query timed out after %s", label, queryTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("%s execution error: %v, output: %s", label, err, truncateOutput(string(out)))
	}
	return truncateOutput(string(out)), nil
}

func appendPostgresConnArgs(args []string, conn *DBConnection) []string {
	if conn.Host != "" {
		args = append(args, "-h", conn.Host)
	}
	if conn.Port != "" {
		args = append(args, "-p", conn.Port)
	}
	if conn.Username != "" {
		args = append(args, "-U", conn.Username)
	}
	return args
}

func appendMySQLConnArgs(args []string, conn *DBConnection) []string {
	if conn.Host != "" {
		args = append(args, "-h", conn.Host)
	}
	if conn.Port != "" {
		args = append(args, "-P", conn.Port)
	}
	if conn.Username != "" {
		args = append(args, "-u", conn.Username)
	}
	return args
}

func postgresEnv(conn *DBConnection) []string {
	env := append(os.Environ(), "PGOPTIONS=-c default_transaction_read_only=on")
	if conn.Password != "" {
		env = append(env, "PGPASSWORD="+conn.Password)
	}
	return env
}

func mysqlEnv(conn *DBConnection) []string {
	env := os.Environ()
	if conn.Password != "" {
		env = append(env, "MYSQL_PWD="+conn.Password)
	}
	return env
}

func safeArgValue(value string) string {
	if strings.HasPrefix(value, "-") {
		return "./" + value
	}
	return value
}

func truncateOutput(out string) string {
	if len(out) <= maxQueryOutput {
		return out
	}
	return out[:maxQueryOutput] + fmt.Sprintf("\n... [truncated %d bytes; add a LIMIT or WHERE clause to narrow the result]", len(out)-maxQueryOutput)
}
