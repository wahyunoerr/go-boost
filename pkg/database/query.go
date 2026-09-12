package database

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	commentRegex        = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*`)
	stringLiteralRegex  = regexp.MustCompile(`'([^'\\]|\\.)*'|"([^"\\]|\\.)*"`)
	allowedLeadingVerbs = regexp.MustCompile(`(?i)^(SELECT|SHOW|EXPLAIN|DESCRIBE|PRAGMA)\b`)
	forbiddenKeywords   = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|RENAME|REPLACE|GRANT|REVOKE|EXEC|EXECUTE)\b`)
)

func ValidateReadOnlyQuery(rawQuery string) (string, error) {
	cleaned := commentRegex.ReplaceAllString(rawQuery, " ")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		return "", fmt.Errorf("empty query provided")
	}

	statements := splitStatementsSafe(cleaned)
	if len(statements) > 1 {
		return "", fmt.Errorf("multiple queries are not allowed for safety")
	}

	singleStmt := strings.TrimSpace(statements[0])

	if !allowedLeadingVerbs.MatchString(singleStmt) {
		return "", fmt.Errorf("only read-only statements (SELECT, SHOW, EXPLAIN, DESCRIBE, PRAGMA) are allowed")
	}

	codeWithoutLiterals := stringLiteralRegex.ReplaceAllString(singleStmt, "''")
	if forbiddenKeywords.MatchString(codeWithoutLiterals) {
		return "", fmt.Errorf("query contains forbidden mutation keywords")
	}

	return singleStmt, nil
}

func splitStatementsSafe(query string) []string {
	var stmts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false

	chars := []rune(query)
	for i := 0; i < len(chars); i++ {
		c := chars[i]

		if c == '\'' && !inDoubleQuote {
			if i == 0 || chars[i-1] != '\\' {
				inSingleQuote = !inSingleQuote
			}
		} else if c == '"' && !inSingleQuote {
			if i == 0 || chars[i-1] != '\\' {
				inDoubleQuote = !inDoubleQuote
			}
		}

		if c == ';' && !inSingleQuote && !inDoubleQuote {
			s := strings.TrimSpace(current.String())
			if s != "" {
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

	upper := strings.ToUpper(safeQuery)
	if strings.HasPrefix(upper, "SELECT") && !strings.Contains(upper, "LIMIT") {
		safeQuery = safeQuery + " LIMIT 100"
	}

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if conn != nil && conn.Driver == "sqlite" && conn.Database != "" {
		if _, err := exec.LookPath("sqlite3"); err != nil {
			return "", fmt.Errorf("sqlite3 CLI is not installed in $PATH")
		}
		cmd := exec.CommandContext(execCtx, "sqlite3", "-header", "-table", conn.Database, safeQuery)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("sqlite execution error: %v, output: %s", err, string(out))
		}
		return string(out), nil
	}

	if conn != nil && conn.Driver == "postgres" && conn.Database != "" {
		if _, err := exec.LookPath("psql"); err != nil {
			return "", fmt.Errorf("psql CLI client is not installed in $PATH to execute PostgreSQL queries directly")
		}
		args := []string{"-d", conn.Database, "-c", safeQuery}
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
			return "", fmt.Errorf("psql execution error: %v, output: %s", err, string(out))
		}
		return string(out), nil
	}

	if conn != nil && conn.Driver == "mysql" && conn.Database != "" {
		args := []string{"-D", conn.Database, "-e", safeQuery}
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
			return "", fmt.Errorf("mysql execution error: %v, output: %s", err, string(out))
		}
		return string(out), nil
	}

	return fmt.Sprintf("Validated Query: %s\n(No active database daemon connected. Provide credentials or run against configured sqlite/postgres/mysql instance)", safeQuery), nil
}
