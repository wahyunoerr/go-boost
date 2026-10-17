package astparser

import (
	"go/ast"
	"os"
	"strings"
)

// ShouldSkipDir reports whether a directory holds code that is not the
// project's own, such as vendor, node_modules, or any dot directory.
func ShouldSkipDir(fi os.FileInfo) bool {
	if fi == nil || !fi.IsDir() {
		return false
	}
	name := fi.Name()
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "third_party" || name == "node_modules"
}

// ShouldSkipFile reports whether a path is not Go source worth analysing.
// Generated files are always skipped; test files are skipped unless
// includeTests is set.
func ShouldSkipFile(path string, includeTests bool) bool {
	if !strings.HasSuffix(path, ".go") {
		return true
	}
	if !includeTests && strings.HasSuffix(path, "_test.go") {
		return true
	}
	if strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, ".gen.go") {
		return true
	}
	return false
}

// IsGeneratedAST reports whether a parsed file carries the standard generated
// code marker that tools are expected to honour.
func IsGeneratedAST(file *ast.File) bool {
	if file == nil {
		return false
	}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			text := strings.ToLower(c.Text)
			if strings.Contains(text, "code generated") && strings.Contains(text, "do not edit") {
				return true
			}
		}
	}
	return false
}
