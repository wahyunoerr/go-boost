package astparser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
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

type declaration struct {
	symbol UnusedSymbol
	key    typeKey
}

func DetectDeadCode(rootDir string) ([]UnusedSymbol, error) {
	files, err := collectAllGoFiles(rootDir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()

	declarations := make([]declaration, 0)
	referencesByPackage := make(map[string]map[string]int)
	importedPackages := make(map[string]bool)

	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}

		pkgName := ""
		if node.Name != nil {
			pkgName = node.Name.Name
		}
		isTestFile := strings.HasSuffix(path, "_test.go")
		relPath := relativeTo(rootDir, path)

		for _, imp := range node.Imports {
			importedPackages[strings.Trim(imp.Path.Value, `"`)] = true
		}

		if !isTestFile {
			for _, decl := range node.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv != nil || d.Name.Name == "main" || d.Name.Name == "init" || d.Name.Name == "_" {
						continue
					}
					declarations = append(declarations, declaration{
						symbol: UnusedSymbol{
							Name:     d.Name.Name,
							Kind:     "func",
							Package:  pkgName,
							File:     relPath,
							Line:     fset.Position(d.Pos()).Line,
							Exported: isExportedName(d.Name.Name),
						},
						key: typeKey{pkg: pkgName, name: d.Name.Name},
					})

				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok || ts.Name.Name == "_" {
							continue
						}
						kind := "type"
						switch ts.Type.(type) {
						case *ast.StructType:
							kind = "struct"
						case *ast.InterfaceType:
							kind = "interface"
						}
						declarations = append(declarations, declaration{
							symbol: UnusedSymbol{
								Name:     ts.Name.Name,
								Kind:     kind,
								Package:  pkgName,
								File:     relPath,
								Line:     fset.Position(ts.Pos()).Line,
								Exported: isExportedName(ts.Name.Name),
							},
							key: typeKey{pkg: pkgName, name: ts.Name.Name},
						})
					}
				}
			}
		}

		if referencesByPackage[pkgName] == nil {
			referencesByPackage[pkgName] = make(map[string]int)
		}
		countReferences(node, referencesByPackage[pkgName])
	}

	unused := make([]UnusedSymbol, 0)
	for _, decl := range declarations {
		if referencesByPackage[decl.key.pkg][decl.key.name] > 0 {
			continue
		}
		unused = append(unused, decl.symbol)
	}

	sort.SliceStable(unused, func(i, j int) bool {
		if unused[i].File != unused[j].File {
			return unused[i].File < unused[j].File
		}
		return unused[i].Line < unused[j].Line
	})

	return unused, nil
}

func countReferences(file *ast.File, counts map[string]int) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Recv != nil {
				countExprReferences(node.Recv, counts)
			}
			countExprReferences(node.Type, counts)
			if node.Body != nil {
				countExprReferences(node.Body, counts)
			}
			return false

		case *ast.GenDecl:
			if node.Tok != token.TYPE {
				countExprReferences(node, counts)
				return false
			}
			for _, spec := range node.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				countExprReferences(ts.Type, counts)
				if ts.TypeParams != nil {
					countExprReferences(ts.TypeParams, counts)
				}
			}
			return false
		}
		return true
	})
}

func countExprReferences(node ast.Node, counts map[string]int) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			countExprReferences(t.X, counts)
			return false
		case *ast.KeyValueExpr:
			if _, isIdent := t.Key.(*ast.Ident); isIdent {
				countExprReferences(t.Value, counts)
				return false
			}
		case *ast.Field:
			countExprReferences(t.Type, counts)
			return false
		case *ast.Ident:
			counts[t.Name]++
		}
		return true
	})
}

func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}

func collectAllGoFiles(rootDir string) ([]string, error) {
	files, err := collectGoFilesWithTests(rootDir)
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
