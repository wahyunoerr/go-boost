package astparser

import (
	"go/ast"
	"os"
	"strings"
)

func ShouldSkipDir(fi os.FileInfo) bool {
	if fi == nil || !fi.IsDir() {
		return false
	}
	name := fi.Name()
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "third_party" || name == "node_modules"
}

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
