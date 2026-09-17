package security

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	secretNameRegex = regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret|passwd|password|jwt[_-]?secret|auth[_-]?token|access[_-]?token|refresh[_-]?token|client[_-]?secret|db[_-]?pass|private[_-]?key|credential)`)

	secretValueRegex = regexp.MustCompile(`(AKIA[0-9A-Z]{16}|ghp_[a-zA-Z0-9]{36}|gho_[a-zA-Z0-9]{36}|ghu_[a-zA-Z0-9]{36}|ghs_[a-zA-Z0-9]{36}|github_pat_[a-zA-Z0-9_]{22,}|glpat-[0-9A-Za-z_-]{20,}|sk_live_[0-9a-zA-Z]{24}|pk_live_[0-9a-zA-Z]{24}|rk_live_[0-9a-zA-Z]{24}|sk-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35}|npm_[A-Za-z0-9]{36}|xox[baprs]-[0-9a-zA-Z-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|eyJ[A-Za-z0-9_=-]+\.[A-Za-z0-9_=-]+\.[A-Za-z0-9_.+/=-]*)`)

	sqlKeywordRegex = regexp.MustCompile(`(?i)\b(SELECT|INSERT\s+INTO|UPDATE|DELETE\s+FROM|WHERE|FROM|JOIN|ORDER\s+BY|VALUES)\b`)

	placeholderValues = map[string]bool{
		"test": true, "secret": true, "changeme": true, "password": true,
		"example": true, "placeholder": true, "your-secret-here": true,
		"todo": true, "xxx": true, "dummy": true, "redacted": true,
	}
)

func ScanCodebase(rootDir string) ([]SecurityVulnerability, error) {
	fset := token.NewFileSet()
	var vulns []SecurityVulnerability

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			if path != rootDir && shouldSkipDir(fi.Name()) {
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

		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			vulns = append(vulns, SecurityVulnerability{
				Type:        "scan_error",
				Severity:    "LOW",
				File:        relativePath(rootDir, path),
				Line:        0,
				Message:     "File could not be parsed and was not scanned: " + err.Error(),
				Remediation: "Fix the syntax error so this file can be audited.",
			})
			return nil
		}

		vulns = append(vulns, inspectFileSecurity(node, fset, relativePath(rootDir, path))...)
		return nil
	})

	return vulns, err
}

func shouldSkipDir(name string) bool {
	return strings.HasPrefix(name, ".") ||
		name == "vendor" ||
		name == "third_party" ||
		name == "node_modules" ||
		name == "testdata"
}

func relativePath(rootDir, path string) string {
	if rel, err := filepath.Rel(rootDir, path); err == nil {
		return rel
	}
	return path
}

type fileScanner struct {
	fset     *token.FileSet
	filePath string
	vulns    []SecurityVulnerability

	sqlTainted map[string]bool
}

func inspectFileSecurity(file *ast.File, fset *token.FileSet, filePath string) []SecurityVulnerability {
	s := &fileScanner{fset: fset, filePath: filePath, sqlTainted: map[string]bool{}}

	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok {
			s.checkValueSpecs(gd)
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				s.scanFunctionBody(fn.Body)
			}
			return false
		case *ast.FuncLit:
			if fn.Body != nil {
				s.scanFunctionBody(fn.Body)
			}
			return false
		}
		return true
	})

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return false
		case *ast.KeyValueExpr:
			s.checkKeyValue(node)
		case *ast.BinaryExpr:
			s.checkConcatenatedLiteral(node)
		case *ast.BasicLit:
			s.checkLiteralValue(node)
		}
		return true
	})

	return s.vulns
}

func (s *fileScanner) scanFunctionBody(body *ast.BlockStmt) {
	previous := s.sqlTainted
	s.sqlTainted = map[string]bool{}
	defer func() { s.sqlTainted = previous }()

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			s.trackSQLAssignment(node)
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if i < len(node.Values) && isDynamicSQL(node.Values[i]) {
					s.sqlTainted[name.Name] = true
				}
			}
		}
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			s.checkCall(node)
		case *ast.KeyValueExpr:
			s.checkKeyValue(node)
		case *ast.AssignStmt:
			s.checkAssignment(node)
		case *ast.ValueSpec:
			s.checkValueSpecNames(node)
		case *ast.BinaryExpr:
			s.checkConcatenatedLiteral(node)
		case *ast.BasicLit:
			s.checkLiteralValue(node)
		}
		return true
	})
}

func (s *fileScanner) trackSQLAssignment(node *ast.AssignStmt) {
	for i, lhs := range node.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || i >= len(node.Rhs) {
			continue
		}
		if isDynamicSQL(node.Rhs[i]) {
			s.sqlTainted[ident.Name] = true
			continue
		}
		if node.Tok == token.ADD_ASSIGN && s.sqlTainted[ident.Name] {
			continue
		}
		if ident2, ok := node.Rhs[i].(*ast.Ident); ok && s.sqlTainted[ident2.Name] {
			s.sqlTainted[ident.Name] = true
		}
	}
}

func isDynamicSQL(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		text, hasDynamic := flattenConcat(e)
		return hasDynamic && sqlKeywordRegex.MatchString(text)
	case *ast.CallExpr:
		name := exprToString(e.Fun)
		if name != "fmt.Sprintf" && name != "fmt.Sprint" {
			return false
		}
		if len(e.Args) == 0 {
			return false
		}
		format, ok := stringLiteralValue(e.Args[0])
		if !ok {
			return false
		}
		return len(e.Args) > 1 && sqlKeywordRegex.MatchString(format)
	}
	return false
}

func flattenConcat(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", true
		}
		leftText, leftDyn := flattenConcat(e.X)
		rightText, rightDyn := flattenConcat(e.Y)
		return leftText + rightText, leftDyn || rightDyn
	case *ast.BasicLit:
		if value, ok := stringLiteralValue(e); ok {
			return value, false
		}
		return "", true
	case *ast.ParenExpr:
		return flattenConcat(e.X)
	default:
		return "", true
	}
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return strings.Trim(lit.Value, "`\""), true
	}
	return value, true
}

func (s *fileScanner) add(pos token.Pos, v SecurityVulnerability) {
	v.File = s.filePath
	v.Line = s.fset.Position(pos).Line
	s.vulns = append(s.vulns, v)
}

func (s *fileScanner) checkCall(node *ast.CallExpr) {
	callName := exprToString(node.Fun)
	lowerCall := strings.ToLower(callName)

	s.checkSQLInjection(node, callName, lowerCall)
	s.checkCommandExecution(node, callName, lowerCall)
	s.checkFileAccess(node, callName, lowerCall)
}

func (s *fileScanner) checkSQLInjection(node *ast.CallExpr, callName, lowerCall string) {
	if !isDBQueryMethod(lowerCall) || len(node.Args) == 0 {
		return
	}

	queryArgIdx := 0
	if strings.HasSuffix(lowerCall, "context") && len(node.Args) > 1 {
		queryArgIdx = 1
	}
	if queryArgIdx >= len(node.Args) {
		return
	}

	target := node.Args[queryArgIdx]

	switch arg := target.(type) {
	case *ast.BinaryExpr:
		if isDynamicSQL(arg) {
			s.add(node.Pos(), SecurityVulnerability{
				Type:        "sql_injection",
				Severity:    "CRITICAL",
				Message:     "Potential SQL injection via string concatenation in query call: " + callName,
				Remediation: "Use parameterized queries with placeholder arguments (?, $1) instead of string concatenation.",
			})
		}
	case *ast.CallExpr:
		if isDynamicSQL(arg) {
			s.add(node.Pos(), SecurityVulnerability{
				Type:        "sql_injection",
				Severity:    "CRITICAL",
				Message:     "Potential SQL injection via fmt.Sprintf formatting in query call: " + callName,
				Remediation: "Pass variables as separate parameterized arguments to the SQL driver instead of formatting them into the query string.",
			})
		}
	case *ast.Ident:
		if s.sqlTainted[arg.Name] {
			s.add(node.Pos(), SecurityVulnerability{
				Type:        "sql_injection",
				Severity:    "CRITICAL",
				Message:     "Potential SQL injection: query variable '" + arg.Name + "' is built from a dynamic string and passed to " + callName,
				Remediation: "Build the statement with placeholders and pass the values as query arguments instead of interpolating them into '" + arg.Name + "'.",
			})
		}
	}
}

func (s *fileScanner) checkCommandExecution(node *ast.CallExpr, callName, lowerCall string) {
	isCommand := strings.HasSuffix(lowerCall, "exec.command")
	isCommandContext := strings.HasSuffix(lowerCall, "exec.commandcontext")
	if !isCommand && !isCommandContext {
		return
	}

	binIdx := 0
	if isCommandContext {
		binIdx = 1
	}
	if len(node.Args) <= binIdx {
		return
	}

	binArg := node.Args[binIdx]
	if !isConstantExpr(binArg) {
		s.add(node.Pos(), SecurityVulnerability{
			Type:        "command_injection",
			Severity:    "CRITICAL",
			Message:     "Dynamic binary executable invocation in command call: " + callName,
			Remediation: "Ensure the executable path is a static constant or strictly allowlisted.",
		})
		return
	}

	binStr, _ := stringLiteralValue(binArg)
	if !isShellBinary(strings.ToLower(binStr)) {
		return
	}

	for _, subArg := range node.Args[binIdx+1:] {
		if isConstantExpr(subArg) {
			continue
		}
		s.add(node.Pos(), SecurityVulnerability{
			Type:        "command_injection",
			Severity:    "CRITICAL",
			Message:     "Potential command injection: a non-constant argument is passed to the shell in " + callName,
			Remediation: "Do not pass dynamic strings to a shell. Invoke the target binary directly and pass each argument as a separate element.",
		})
		return
	}
}

func (s *fileScanner) checkFileAccess(node *ast.CallExpr, callName, lowerCall string) {
	isFileOpen := strings.HasSuffix(lowerCall, "os.open") ||
		strings.HasSuffix(lowerCall, "os.readfile") ||
		strings.HasSuffix(lowerCall, "os.create") ||
		strings.HasSuffix(lowerCall, "os.openfile") ||
		strings.HasSuffix(lowerCall, "os.writefile")
	if !isFileOpen || len(node.Args) == 0 {
		return
	}

	arg := node.Args[0]
	dynamic := false
	switch a := arg.(type) {
	case *ast.BinaryExpr:
		dynamic = true
	case *ast.CallExpr:
		switch exprToString(a.Fun) {
		case "fmt.Sprintf", "fmt.Sprint":
			dynamic = true
		case "filepath.Join", "path.Join":
			dynamic = hasDynamicTrailingSegment(a.Args)
		}
	}

	if !dynamic {
		return
	}

	s.add(node.Pos(), SecurityVulnerability{
		Type:        "path_traversal",
		Severity:    "HIGH",
		Message:     "File path is built from a dynamic value in " + callName + "; a caller-controlled value could escape the intended directory",
		Remediation: "Confine the access to a base directory with os.Root (Go 1.24+), or reject the input unless filepath.IsLocal reports true. filepath.Clean alone does not prevent traversal.",
	})
}

func hasDynamicTrailingSegment(args []ast.Expr) bool {
	for i, arg := range args {
		if i == 0 {
			continue
		}
		if !isConstantExpr(arg) {
			return true
		}
	}
	return false
}

func (s *fileScanner) checkKeyValue(node *ast.KeyValueExpr) {
	keyName := exprToString(node.Key)

	if keyName == "InsecureSkipVerify" {
		if ident, ok := node.Value.(*ast.Ident); ok && ident.Name == "true" {
			s.add(node.Pos(), SecurityVulnerability{
				Type:        "insecure_tls",
				Severity:    "HIGH",
				Message:     "InsecureSkipVerify set to true disables TLS certificate verification",
				Remediation: "Enable TLS certificate verification in production, or supply a custom root CA pool via tls.Config.RootCAs.",
			})
		}
		return
	}

	s.checkSecretPair(node.Pos(), keyName, node.Value)
}

func (s *fileScanner) checkAssignment(node *ast.AssignStmt) {
	for i, lhs := range node.Lhs {
		if i >= len(node.Rhs) {
			break
		}
		name := exprToString(lhs)

		if strings.HasSuffix(name, "InsecureSkipVerify") {
			if ident, ok := node.Rhs[i].(*ast.Ident); ok && ident.Name == "true" {
				s.add(node.Pos(), SecurityVulnerability{
					Type:        "insecure_tls",
					Severity:    "HIGH",
					Message:     "InsecureSkipVerify set to true disables TLS certificate verification",
					Remediation: "Enable TLS certificate verification in production, or supply a custom root CA pool via tls.Config.RootCAs.",
				})
			}
			continue
		}

		s.checkSecretPair(node.Pos(), name, node.Rhs[i])
	}
}

func (s *fileScanner) checkValueSpecs(gd *ast.GenDecl) {
	if gd.Tok != token.CONST && gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		s.checkValueSpecNames(vs)
	}
}

func (s *fileScanner) checkValueSpecNames(vs *ast.ValueSpec) {
	for i, name := range vs.Names {
		if i >= len(vs.Values) {
			break
		}
		s.checkSecretPair(vs.Pos(), name.Name, vs.Values[i])
	}
}

func (s *fileScanner) checkSecretPair(pos token.Pos, name string, value ast.Expr) {
	if name == "" || !secretNameRegex.MatchString(name) {
		return
	}

	literal, ok := stringLiteralValue(value)
	if !ok {
		return
	}
	if !isLikelyRealSecret(literal) {
		return
	}

	s.add(pos, SecurityVulnerability{
		Type:        "hardcoded_secret",
		Severity:    "HIGH",
		Message:     "Suspected hardcoded secret or credential in '" + name + "'",
		Remediation: "Load secrets from environment variables or a secret manager, and rotate this value if it was ever real.",
	})
}

func (s *fileScanner) checkConcatenatedLiteral(node *ast.BinaryExpr) {
	if node.Op != token.ADD {
		return
	}
	value, hasDynamic := flattenConcat(node)
	if hasDynamic || value == "" {
		return
	}
	if !secretValueRegex.MatchString(value) {
		return
	}

	s.add(node.Pos(), SecurityVulnerability{
		Type:        "hardcoded_secret",
		Severity:    "CRITICAL",
		Message:     "Found a hardcoded credential assembled from concatenated string literals",
		Remediation: "Remove the credential from source control and rotate the exposed key immediately. Splitting a key across concatenated literals hides it from most scanners but not from anyone reading the code.",
	})
}

func (s *fileScanner) checkLiteralValue(node *ast.BasicLit) {
	if node.Kind != token.STRING {
		return
	}
	value, ok := stringLiteralValue(node)
	if !ok {
		return
	}
	if !secretValueRegex.MatchString(value) {
		return
	}

	s.add(node.Pos(), SecurityVulnerability{
		Type:        "hardcoded_secret",
		Severity:    "CRITICAL",
		Message:     "Found a hardcoded API key, token, or private key in a string literal",
		Remediation: "Remove the credential from source control and rotate the exposed key immediately.",
	})
}

func isLikelyRealSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 8 {
		return false
	}
	if placeholderValues[strings.ToLower(trimmed)] {
		return false
	}
	if strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "{{") {
		return false
	}
	return true
}

func isShellBinary(bin string) bool {
	switch bin {
	case "sh", "bash", "zsh", "ksh", "dash", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh":
		return true
	}
	return strings.HasSuffix(bin, "/sh") || strings.HasSuffix(bin, "/bash") || strings.HasSuffix(bin, "/zsh")
}

func isConstantExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return isConstantExpr(e.X)
	case *ast.BinaryExpr:
		return isConstantExpr(e.X) && isConstantExpr(e.Y)
	default:
		return false
	}
}

func isDBQueryMethod(lower string) bool {
	methods := []string{
		".query", ".querycontext", ".queryrow", ".queryrowcontext",
		".exec", ".execcontext", ".raw", ".where", ".select", ".selectcontext",
		".get", ".getcontext", ".mustexec", ".queryx", ".queryrowx", ".namedexec",
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
	case *ast.CallExpr:
		return exprToString(t.Fun) + "()"
	case *ast.IndexExpr:
		return exprToString(t.X)
	default:
		return ""
	}
}

const BaselineFile = ".go-boost-security.baseline.json"

type Baseline struct {
	Accepted []BaselineEntry `json:"accepted"`
}

type BaselineEntry struct {
	Type    string `json:"type"`
	File    string `json:"file"`
	Message string `json:"message"`
}

func (v SecurityVulnerability) fingerprint() BaselineEntry {
	return BaselineEntry{Type: v.Type, File: v.File, Message: v.Message}
}

func LoadBaseline(rootDir string) (*Baseline, error) {
	data, err := os.ReadFile(filepath.Join(rootDir, BaselineFile))
	if err != nil {
		if os.IsNotExist(err) {
			return &Baseline{}, nil
		}
		return nil, err
	}

	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", BaselineFile, err)
	}
	return &b, nil
}

func WriteBaseline(rootDir string, vulns []SecurityVulnerability) (string, error) {
	entries := make([]BaselineEntry, 0, len(vulns))
	seen := map[BaselineEntry]bool{}
	for _, v := range vulns {
		fp := v.fingerprint()
		if seen[fp] {
			continue
		}
		seen[fp] = true
		entries = append(entries, fp)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].Message < entries[j].Message
	})

	data, err := json.MarshalIndent(Baseline{Accepted: entries}, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	path := filepath.Join(rootDir, BaselineFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func ApplyBaseline(vulns []SecurityVulnerability, baseline *Baseline) (remaining []SecurityVulnerability, accepted int) {
	if baseline == nil || len(baseline.Accepted) == 0 {
		return vulns, 0
	}

	known := make(map[BaselineEntry]bool, len(baseline.Accepted))
	for _, e := range baseline.Accepted {
		known[e] = true
	}

	remaining = make([]SecurityVulnerability, 0, len(vulns))
	for _, v := range vulns {
		if known[v.fingerprint()] {
			accepted++
			continue
		}
		remaining = append(remaining, v)
	}
	return remaining, accepted
}
