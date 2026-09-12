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
		if err != nil || fi.IsDir() {
			if ShouldSkipDir(fi) {
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

		fileIssues := inspectFileConcurrency(node, fset, path)
		issues = append(issues, fileIssues...)
		return nil
	})

	return issues, err
}

func inspectFileConcurrency(file *ast.File, fset *token.FileSet, filePath string) []ConcurrencyIssue {
	var issues []ConcurrencyIssue

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}

		var cancelVarNames []string
		var hasDeferCancel bool
		var lockCalls []string
		var unlockDefers []string
		var manualUnlocks []string
		var forLoopGoroutines int

		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			switch node := inner.(type) {
			case *ast.AssignStmt:
				for _, rhs := range node.Rhs {
					if call, ok := rhs.(*ast.CallExpr); ok {
						callName := exprToString(call.Fun)
						if strings.Contains(callName, "context.WithTimeout") ||
							strings.Contains(callName, "context.WithCancel") ||
							strings.Contains(callName, "context.WithDeadline") {
							if len(node.Lhs) >= 2 {
								cancelVar := exprToString(node.Lhs[1])
								cancelVarNames = append(cancelVarNames, cancelVar)
							}
						}
					}
				}

			case *ast.ExprStmt:
				if call, ok := node.X.(*ast.CallExpr); ok {
					callName := exprToString(call.Fun)
					if strings.HasSuffix(callName, ".Lock") || strings.HasSuffix(callName, ".RLock") {
						lockCalls = append(lockCalls, callName)
					} else if strings.HasSuffix(callName, ".Unlock") || strings.HasSuffix(callName, ".RUnlock") {
						manualUnlocks = append(manualUnlocks, callName)
					}
				}

			case *ast.DeferStmt:
				deferCallName := exprToString(node.Call.Fun)
				for _, cName := range cancelVarNames {
					if deferCallName == cName {
						hasDeferCancel = true
					}
				}
				if strings.HasSuffix(deferCallName, ".Unlock") || strings.HasSuffix(deferCallName, ".RUnlock") {
					unlockDefers = append(unlockDefers, deferCallName)
				}

			case *ast.ForStmt, *ast.RangeStmt:
				ast.Inspect(node, func(loopNode ast.Node) bool {
					if _, ok := loopNode.(*ast.GoStmt); ok {
						forLoopGoroutines++
					}
					return true
				})
			}

			return true
		})

		if len(cancelVarNames) > 0 && !hasDeferCancel {
			pos := fset.Position(fn.Pos())
			issues = append(issues, ConcurrencyIssue{
				Type:        "context_leak",
				Severity:    "HIGH",
				File:        filePath,
				Line:        pos.Line,
				Message:     "context.WithCancel or WithTimeout called without defer cancel() in function " + fn.Name.Name,
				Remediation: "Add `defer cancel()` immediately after context creation to prevent context timer leaks.",
			})
		}

		totalUnlocks := len(unlockDefers) + len(manualUnlocks)
		if len(lockCalls) > totalUnlocks {
			pos := fset.Position(fn.Pos())
			issues = append(issues, ConcurrencyIssue{
				Type:        "mutex_leak",
				Severity:    "MEDIUM",
				File:        filePath,
				Line:        pos.Line,
				Message:     "Mutex Lock called without matching Unlock in function " + fn.Name.Name,
				Remediation: "Ensure every `mu.Lock()` has a matching `defer mu.Unlock()` or manual `mu.Unlock()` call.",
			})
		}

		return true
	})

	return issues
}
