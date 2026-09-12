package security

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SecurityVulnerability struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Message     string `json:"message"`
	CodeSnippet string `json:"code_snippet,omitempty"`
	Remediation string `json:"remediation"`
}

var (
	secretNameRegex  = regexp.MustCompile(`(?i)(api[_-]?key|jwt[_-]?secret|auth[_-]?token|db[_-]?pass|private[_-]?key)`)
	secretValueRegex = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}|ghp_[a-zA-Z0-9]{36}|sk_live_[0-9a-zA-Z]{24}|eyJ[A-Za-z0-9-_=]+\.[A-Za-z0-9-_=]+\.?[A-Za-z0-9-_.+/=]*)`)
)

func ScanCodebase(rootDir string) ([]SecurityVulnerability, error) {
	fset := token.NewFileSet()
	var vulns []SecurityVulnerability

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			if fi != nil && (strings.HasPrefix(fi.Name(), ".") || fi.Name() == "vendor" || fi.Name() == "third_party" || fi.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, ".gen.go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		fileVulns := inspectFileSecurity(node, fset, path)
		vulns = append(vulns, fileVulns...)
		return nil
	})

	return vulns, err
}

func inspectFileSecurity(file *ast.File, fset *token.FileSet, filePath string) []SecurityVulnerability {
	var vulns []SecurityVulnerability

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			callName := exprToString(node.Fun)
			lowerCall := strings.ToLower(callName)

			if isDBQueryMethod(lowerCall) && len(node.Args) > 0 {
				queryArgIdx := 0
				if strings.Contains(lowerCall, "context") && len(node.Args) > 1 {
					queryArgIdx = 1
				}

				if queryArgIdx < len(node.Args) {
					targetArg := node.Args[queryArgIdx]

					if _, ok := targetArg.(*ast.BinaryExpr); ok {
						pos := fset.Position(node.Pos())
						vulns = append(vulns, SecurityVulnerability{
							Type:        "sql_injection",
							Severity:    "CRITICAL",
							File:        filePath,
							Line:        pos.Line,
							Message:     "Potential SQL injection via string concatenation in query call: " + callName,
							Remediation: "Use parameterized queries with placeholder arguments (?, $1) instead of string concatenation.",
						})
					}

					if innerCall, ok := targetArg.(*ast.CallExpr); ok {
						innerName := exprToString(innerCall.Fun)
						if innerName == "fmt.Sprintf" || innerName == "fmt.Sprint" {
							pos := fset.Position(node.Pos())
							vulns = append(vulns, SecurityVulnerability{
								Type:        "sql_injection",
								Severity:    "CRITICAL",
								File:        filePath,
								Line:        pos.Line,
								Message:     "Potential SQL injection via fmt.Sprintf formatting in query call: " + callName,
								Remediation: "Pass variables as separate parameterized arguments to the SQL driver instead of formatting them into the query string.",
							})
						}
					}
				}
			}

			if (strings.HasSuffix(lowerCall, "exec.command") || strings.HasSuffix(lowerCall, "exec.commandcontext")) && len(node.Args) > 0 {
				binIdx := 0
				if strings.HasSuffix(lowerCall, "exec.commandcontext") {
					binIdx = 1
				}

				if len(node.Args) > binIdx {
					binArg := node.Args[binIdx]
					binDynamic := false
					if _, ok := binArg.(*ast.BinaryExpr); ok {
						binDynamic = true
					} else if innerCall, ok := binArg.(*ast.CallExpr); ok {
						innerName := exprToString(innerCall.Fun)
						if innerName == "fmt.Sprintf" || innerName == "fmt.Sprint" {
							binDynamic = true
						}
					}

					if binDynamic {
						pos := fset.Position(node.Pos())
						vulns = append(vulns, SecurityVulnerability{
							Type:        "command_injection",
							Severity:    "CRITICAL",
							File:        filePath,
							Line:        pos.Line,
							Message:     "Dynamic binary executable invocation in command call: " + callName,
							Remediation: "Ensure executable binary path is a static constant or strictly whitelisted.",
						})
					} else {
						binStr := ""
						if lit, ok := binArg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							binStr = strings.ToLower(strings.Trim(lit.Value, `"`))
						}
						isShell := binStr == "sh" || binStr == "bash" || binStr == "zsh" || binStr == "cmd" || binStr == "powershell" || strings.HasSuffix(binStr, "/sh") || strings.HasSuffix(binStr, "/bash")

						if isShell {
							for _, subArg := range node.Args[binIdx+1:] {
								hasDynamic := false
								if _, ok := subArg.(*ast.BinaryExpr); ok {
									hasDynamic = true
								} else if innerCall, ok := subArg.(*ast.CallExpr); ok {
									innerName := exprToString(innerCall.Fun)
									if innerName == "fmt.Sprintf" || innerName == "fmt.Sprint" {
										hasDynamic = true
									}
								}
								if hasDynamic {
									pos := fset.Position(node.Pos())
									vulns = append(vulns, SecurityVulnerability{
										Type:        "command_injection",
										Severity:    "CRITICAL",
										File:        filePath,
										Line:        pos.Line,
										Message:     "Potential command injection via dynamic shell argument: " + callName,
										Remediation: "Do not execute dynamic strings in shell commands; invoke target binary directly without shell wrapping.",
									})
									break
								}
							}
						}
					}
				}
			}

			if (strings.HasSuffix(lowerCall, "os.open") || strings.HasSuffix(lowerCall, "os.readfile") || strings.HasSuffix(lowerCall, "os.create")) && len(node.Args) > 0 {
				if _, ok := node.Args[0].(*ast.BinaryExpr); ok {
					pos := fset.Position(node.Pos())
					vulns = append(vulns, SecurityVulnerability{
						Type:        "path_traversal",
						Severity:    "HIGH",
						File:        filePath,
						Line:        pos.Line,
						Message:     "Potential path traversal via unvalidated file path concatenation: " + callName,
						Remediation: "Validate file paths and sanitize with `filepath.Clean` before opening files.",
					})
				}
			}

		case *ast.KeyValueExpr:
			keyName := exprToString(node.Key)
			if keyName == "InsecureSkipVerify" {
				if valIdent, ok := node.Value.(*ast.Ident); ok && valIdent.Name == "true" {
					pos := fset.Position(node.Pos())
					vulns = append(vulns, SecurityVulnerability{
						Type:        "insecure_tls",
						Severity:    "HIGH",
						File:        filePath,
						Line:        pos.Line,
						Message:     "InsecureSkipVerify set to true disables TLS certificate verification",
						Remediation: "Enable TLS certificate verification in production environments or use custom root CAs.",
					})
				}
			}

		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				varName := exprToString(lhs)
				if secretNameRegex.MatchString(varName) && i < len(node.Rhs) {
					if lit, ok := node.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						val := strings.Trim(lit.Value, "\"")
						if len(val) > 8 && val != "test" && val != "secret" && val != "changeme" {
							pos := fset.Position(node.Pos())
							vulns = append(vulns, SecurityVulnerability{
								Type:        "hardcoded_secret",
								Severity:    "HIGH",
								File:        filePath,
								Line:        pos.Line,
								Message:     "Suspected hardcoded secret or credential in variable: " + varName,
								Remediation: "Load secrets and credentials from environment variables or a secret management service.",
							})
						}
					}
				}
			}

		case *ast.BasicLit:
			if node.Kind == token.STRING {
				val := strings.Trim(node.Value, "\"")
				if secretValueRegex.MatchString(val) {
					pos := fset.Position(node.Pos())
					vulns = append(vulns, SecurityVulnerability{
						Type:        "hardcoded_secret",
						Severity:    "CRITICAL",
						File:        filePath,
						Line:        pos.Line,
						Message:     "Found hardcoded API key or token pattern in string literal",
						Remediation: "Remove hardcoded credentials and rotate any exposed keys immediately.",
					})
				}
			}
		}

		return true
	})

	return vulns
}

func isDBQueryMethod(lower string) bool {
	methods := []string{
		".query", ".querycontext", ".queryrow", ".queryrowcontext",
		".exec", ".execcontext", ".raw", ".where", ".select", ".selectcontext",
	}
	for _, m := range methods {
		if strings.HasSuffix(lower, m) {
			return true
		}
	}
	return false
}

func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	default:
		return ""
	}
}
