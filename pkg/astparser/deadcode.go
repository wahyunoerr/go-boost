package astparser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type UnusedSymbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Package  string `json:"package,omitempty"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

func DetectDeadCode(rootDir string) ([]UnusedSymbol, error) {
	fset := token.NewFileSet()

	declared := make(map[string]UnusedSymbol)
	referenced := make(map[string]int)

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			if ShouldSkipDir(fi) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipFile(path, true) {
			return nil
		}

		isTestFile := strings.HasSuffix(path, "_test.go")

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		pkgName := ""
		if node.Name != nil {
			pkgName = node.Name.Name
		}

		if !isTestFile {
			for _, decl := range node.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:

					if d.Recv == nil && d.Name.Name != "main" && d.Name.Name != "init" {
						pos := fset.Position(d.Pos())
						isExp := false
						if len(d.Name.Name) > 0 {
							isExp = unicode.IsUpper(rune(d.Name.Name[0]))
						}
						declared[d.Name.Name] = UnusedSymbol{
							Name:     d.Name.Name,
							Kind:     "func",
							Package:  pkgName,
							File:     path,
							Line:     pos.Line,
							Exported: isExp,
						}
					}
				case *ast.GenDecl:
					if d.Tok == token.TYPE {
						for _, spec := range d.Specs {
							if ts, ok := spec.(*ast.TypeSpec); ok {
								pos := fset.Position(ts.Pos())
								kind := "type"
								if _, ok := ts.Type.(*ast.StructType); ok {
									kind = "struct"
								} else if _, ok := ts.Type.(*ast.InterfaceType); ok {
									kind = "interface"
								}
								isExp := false
								if len(ts.Name.Name) > 0 {
									isExp = unicode.IsUpper(rune(ts.Name.Name[0]))
								}
								declared[ts.Name.Name] = UnusedSymbol{
									Name:     ts.Name.Name,
									Kind:     kind,
									Package:  pkgName,
									File:     path,
									Line:     pos.Line,
									Exported: isExp,
								}
							}
						}
					}
				}
			}
		}

		ast.Inspect(node, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				referenced[ident.Name]++
			}
			return true
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	var unused []UnusedSymbol
	for name, sym := range declared {
		if referenced[name] <= 1 {
			unused = append(unused, sym)
		}
	}

	return unused, nil
}
