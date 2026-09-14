package astparser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type ConcurrencyIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

func CheckConcurrency(rootDir string) ([]ConcurrencyIssue, error) {
	fset := token.NewFileSet()
	issues := make([]ConcurrencyIssue, 0)

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			if path != rootDir && ShouldSkipDir(fi) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipFile(path, false) {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		issues = append(issues, inspectFileConcurrency(node, fset, relativeTo(rootDir, path))...)
		return nil
	})

	sortIssues(issues)
	return issues, err
}

func relativeTo(rootDir, path string) string {
	if rel, err := filepath.Rel(rootDir, path); err == nil {
		return rel
	}
	return path
}

func sortIssues(issues []ConcurrencyIssue) {
	for i := 1; i < len(issues); i++ {
		for j := i; j > 0; j-- {
			a, b := issues[j-1], issues[j]
			if a.File < b.File || (a.File == b.File && a.Line <= b.Line) {
				break
			}
			issues[j-1], issues[j] = issues[j], issues[j-1]
		}
	}
}

type concurrencyScanner struct {
	fset     *token.FileSet
	filePath string
	issues   []ConcurrencyIssue
}

func inspectFileConcurrency(file *ast.File, fset *token.FileSet, filePath string) []ConcurrencyIssue {
	s := &concurrencyScanner{fset: fset, filePath: filePath}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		s.checkFunction(fn.Name.Name, fn.Type, fn.Body)
	}

	return s.issues
}

func (s *concurrencyScanner) add(pos token.Pos, issue ConcurrencyIssue) {
	issue.File = s.filePath
	issue.Line = s.fset.Position(pos).Line
	s.issues = append(s.issues, issue)
}

func (s *concurrencyScanner) checkFunction(name string, ftype *ast.FuncType, body *ast.BlockStmt) {
	s.checkContextCancel(name, ftype, body)
	s.checkMutexPairs(name, body)
	s.checkGoroutinesInLoops(name, body)

	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
			s.checkMutexPairs(name+".func", lit.Body)
			return false
		}
		return true
	})
}

func (s *concurrencyScanner) checkContextCancel(fnName string, ftype *ast.FuncType, body *ast.BlockStmt) {
	type cancelSite struct {
		name string
		pos  token.Pos
	}
	var sites []cancelSite

	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			callName := exprToString(call.Fun)
			if callName != "context.WithTimeout" && callName != "context.WithCancel" && callName != "context.WithDeadline" {
				continue
			}
			if len(assign.Lhs) < 2 {
				continue
			}
			sites = append(sites, cancelSite{name: exprToString(assign.Lhs[1]), pos: assign.Pos()})
		}
		return true
	})

	if len(sites) == 0 {
		return
	}

	used := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.DeferStmt:
			markCancelUse(used, node.Call)
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				used[ident.Name] = true
			}
			for _, arg := range node.Args {
				if ident, ok := arg.(*ast.Ident); ok {
					used[ident.Name] = true
				}
			}
		case *ast.ReturnStmt:
			for _, res := range node.Results {
				if ident, ok := res.(*ast.Ident); ok {
					used[ident.Name] = true
				}
			}
		}
		return true
	})

	for _, site := range sites {
		if site.name == "_" {
			s.add(site.pos, ConcurrencyIssue{
				Type:        "context_leak",
				Severity:    "HIGH",
				Message:     "The cancel function returned by context is discarded with '_' in " + fnName,
				Remediation: "Capture the cancel function and call `defer cancel()`; discarding it leaks the context's timer until the deadline fires.",
			})
			continue
		}
		if !used[site.name] {
			s.add(site.pos, ConcurrencyIssue{
				Type:        "context_leak",
				Severity:    "HIGH",
				Message:     "context cancel function '" + site.name + "' is never called in " + fnName,
				Remediation: "Add `defer " + site.name + "()` immediately after creating the context to release its timer.",
			})
		}
	}
}

func markCancelUse(used map[string]bool, call *ast.CallExpr) {
	if ident, ok := call.Fun.(*ast.Ident); ok {
		used[ident.Name] = true
	}
	if lit, ok := call.Fun.(*ast.FuncLit); ok && lit.Body != nil {
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if inner, ok := n.(*ast.CallExpr); ok {
				if ident, ok := inner.Fun.(*ast.Ident); ok {
					used[ident.Name] = true
				}
			}
			return true
		})
	}
	for _, arg := range call.Args {
		if ident, ok := arg.(*ast.Ident); ok {
			used[ident.Name] = true
		}
	}
}

func (s *concurrencyScanner) checkMutexPairs(fnName string, body *ast.BlockStmt) {
	deferredUnlocks := collectDeferredUnlocks(body)
	s.walkBlockForLocks(fnName, body, deferredUnlocks)
}

func collectDeferredUnlocks(body *ast.BlockStmt) map[string]bool {
	deferred := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			_ = lit
			return false
		}
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if receiver, ok := unlockReceiver(d.Call); ok {
			deferred[receiver] = true
		}
		return true
	})
	return deferred
}

func (s *concurrencyScanner) walkBlockForLocks(fnName string, block *ast.BlockStmt, deferredUnlocks map[string]bool) {
	for i, stmt := range block.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		receiver, isLock := lockReceiver(call)
		if !isLock {
			continue
		}
		if deferredUnlocks[receiver] {
			continue
		}

		rest := block.List[i+1:]
		unlockIdx := indexOfUnlock(rest, receiver)

		if unlockIdx < 0 {
			s.add(call.Pos(), ConcurrencyIssue{
				Type:        "mutex_leak",
				Severity:    "HIGH",
				Message:     "`" + receiver + ".Lock()` in " + fnName + " is never released in this block",
				Remediation: "Call `defer " + strings.TrimSuffix(receiver, "()") + ".Unlock()` immediately after locking so the mutex is released on every path.",
			})
			continue
		}

		if exitStmt := findEarlyExit(rest[:unlockIdx]); exitStmt != nil {
			s.add(exitStmt.Pos(), ConcurrencyIssue{
				Type:        "mutex_leak",
				Severity:    "HIGH",
				Message:     "`" + receiver + "` stays locked on an early return path in " + fnName,
				Remediation: "Use `defer " + strings.TrimSuffix(receiver, "()") + ".Unlock()` right after locking; a manual Unlock is skipped whenever the function returns early.",
			})
		}
	}

	for _, stmt := range block.List {
		switch node := stmt.(type) {
		case *ast.IfStmt:
			if node.Body != nil {
				s.walkBlockForLocks(fnName, node.Body, deferredUnlocks)
			}
		case *ast.ForStmt:
			if node.Body != nil {
				s.walkBlockForLocks(fnName, node.Body, deferredUnlocks)
			}
		case *ast.RangeStmt:
			if node.Body != nil {
				s.walkBlockForLocks(fnName, node.Body, deferredUnlocks)
			}
		case *ast.BlockStmt:
			s.walkBlockForLocks(fnName, node, deferredUnlocks)
		}
	}
}

func lockReceiver(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if sel.Sel.Name != "Lock" && sel.Sel.Name != "RLock" {
		return "", false
	}
	return exprToString(sel.X), true
}

func unlockReceiver(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if sel.Sel.Name != "Unlock" && sel.Sel.Name != "RUnlock" {
		return "", false
	}
	return exprToString(sel.X), true
}

func indexOfUnlock(stmts []ast.Stmt, receiver string) int {
	for i, stmt := range stmts {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		if got, ok := unlockReceiver(call); ok && got == receiver {
			return i
		}
	}
	return -1
}

func findEarlyExit(stmts []ast.Stmt) ast.Stmt {
	var found ast.Stmt
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			switch node := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				found = node
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func (s *concurrencyScanner) checkGoroutinesInLoops(fnName string, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		var loopBody *ast.BlockStmt
		switch node := n.(type) {
		case *ast.ForStmt:
			loopBody = node.Body
		case *ast.RangeStmt:
			loopBody = node.Body
		default:
			return true
		}
		if loopBody == nil {
			return true
		}

		var goStmt *ast.GoStmt
		ast.Inspect(loopBody, func(inner ast.Node) bool {
			if g, ok := inner.(*ast.GoStmt); ok && goStmt == nil {
				goStmt = g
				return false
			}
			return true
		})
		if goStmt == nil {
			return true
		}

		if hasSynchronization(body) {
			return true
		}

		s.add(goStmt.Pos(), ConcurrencyIssue{
			Type:        "unsupervised_goroutine",
			Severity:    "MEDIUM",
			Message:     "Goroutine started inside a loop in " + fnName + " with no visible WaitGroup, errgroup, or channel to wait on",
			Remediation: "Track the goroutines with sync.WaitGroup or golang.org/x/sync/errgroup, and give each one a termination path via ctx.Done() so none outlive the caller.",
		})
		return true
	})
}

func hasSynchronization(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := exprToString(call.Fun)
		switch {
		case strings.HasSuffix(name, ".Wait"),
			strings.HasSuffix(name, ".Add"),
			strings.HasSuffix(name, ".Done"),
			strings.HasSuffix(name, ".Go"),
			strings.HasSuffix(name, ".Acquire"),
			strings.Contains(name, "errgroup"):
			found = true
			return false
		}
		return true
	})
	return found
}
