package astparser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

type StructInfo struct {
	Name    string      `json:"name"`
	Package string      `json:"package,omitempty"`
	Doc     string      `json:"doc,omitempty"`
	File    string      `json:"file"`
	Line    int         `json:"line"`
	Fields  []FieldInfo `json:"fields"`
	Methods []string    `json:"methods,omitempty"`
}

type FieldInfo struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Tag        string            `json:"tag,omitempty"`
	ParsedTags map[string]string `json:"parsed_tags,omitempty"`
	Doc        string            `json:"doc,omitempty"`
	Embedded   bool              `json:"embedded,omitempty"`
}

type InterfaceInfo struct {
	Name    string            `json:"name"`
	Package string            `json:"package,omitempty"`
	Doc     string            `json:"doc,omitempty"`
	File    string            `json:"file"`
	Line    int               `json:"line"`
	Methods []InterfaceMethod `json:"methods"`
}

type InterfaceMethod struct {
	Name    string   `json:"name"`
	Params  []string `json:"params"`
	Returns []string `json:"returns"`
	Doc     string   `json:"doc,omitempty"`
}

type PackageSymbols struct {
	Package    string          `json:"package"`
	Structs    []StructInfo    `json:"structs"`
	Interfaces []InterfaceInfo `json:"interfaces"`
}

func ParsePath(targetPath string) (*PackageSymbols, error) {
	fset := token.NewFileSet()
	symbols := &PackageSymbols{
		Structs:    make([]StructInfo, 0),
		Interfaces: make([]InterfaceInfo, 0),
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, fmt.Errorf("stat path error: %w", err)
	}

	if !info.IsDir() {
		node, err := parser.ParseFile(fset, targetPath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse file error: %w", err)
		}
		symbols.Package = node.Name.Name
		inspectFile(node, fset, targetPath, symbols)
		return symbols, nil
	}

	var filesToParse []string
	err = filepath.Walk(targetPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if ShouldSkipDir(fi) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipFile(path, false) {
			return nil
		}
		filesToParse = append(filesToParse, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(filesToParse) == 0 {
		return symbols, nil
	}

	numWorkers := runtime.NumCPU()
	if numWorkers > len(filesToParse) {
		numWorkers = len(filesToParse)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	fileChan := make(chan string, len(filesToParse))
	for _, f := range filesToParse {
		fileChan <- f
	}
	close(fileChan)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localFset := token.NewFileSet()
			localSymbols := &PackageSymbols{
				Structs:    make([]StructInfo, 0),
				Interfaces: make([]InterfaceInfo, 0),
			}

			for path := range fileChan {
				node, err := parser.ParseFile(localFset, path, nil, parser.ParseComments)
				if err != nil || IsGeneratedAST(node) {
					continue
				}
				if localSymbols.Package == "" && node.Name != nil {
					localSymbols.Package = node.Name.Name
				}
				inspectFile(node, localFset, path, localSymbols)
			}

			mu.Lock()
			if symbols.Package == "" && localSymbols.Package != "" {
				symbols.Package = localSymbols.Package
			}
			symbols.Structs = append(symbols.Structs, localSymbols.Structs...)
			symbols.Interfaces = append(symbols.Interfaces, localSymbols.Interfaces...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return symbols, nil
}

func inspectFile(file *ast.File, fset *token.FileSet, filePath string, symbols *PackageSymbols) {
	pkgName := ""
	if file.Name != nil {
		pkgName = file.Name.Name
	}

	structMethods := make(map[string][]string)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvName := extractReceiverName(fn.Recv.List[0].Type)
			if recvName != "" {
				structMethods[recvName] = append(structMethods[recvName], fn.Name.Name)
			}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			line := fset.Position(typeSpec.Pos()).Line
			doc := cleanDoc(genDecl.Doc)

			switch t := typeSpec.Type.(type) {
			case *ast.StructType:
				stInfo := StructInfo{
					Name:    typeSpec.Name.Name,
					Package: pkgName,
					Doc:     doc,
					File:    filePath,
					Line:    line,
					Fields:  make([]FieldInfo, 0),
					Methods: structMethods[typeSpec.Name.Name],
				}

				if t.Fields != nil {
					for _, field := range t.Fields.List {
						fieldName := ""
						isEmbedded := false
						if len(field.Names) > 0 {
							fieldName = field.Names[0].Name
						} else {
							isEmbedded = true
							fieldName = strings.TrimPrefix(exprToString(field.Type), "*")
						}

						rawTag := ""
						tagMap := make(map[string]string)
						if field.Tag != nil {
							rawTag = strings.Trim(field.Tag.Value, "`")
							tagMap = parseStructTags(rawTag)
						}

						stInfo.Fields = append(stInfo.Fields, FieldInfo{
							Name:       fieldName,
							Type:       exprToString(field.Type),
							Tag:        rawTag,
							ParsedTags: tagMap,
							Doc:        cleanDoc(field.Doc),
							Embedded:   isEmbedded,
						})
					}
				}
				symbols.Structs = append(symbols.Structs, stInfo)

			case *ast.InterfaceType:
				ifInfo := InterfaceInfo{
					Name:    typeSpec.Name.Name,
					Package: pkgName,
					Doc:     doc,
					File:    filePath,
					Line:    line,
					Methods: make([]InterfaceMethod, 0),
				}

				if t.Methods != nil {
					for _, m := range t.Methods.List {
						ft, ok := m.Type.(*ast.FuncType)
						if !ok || len(m.Names) == 0 {
							continue
						}

						methodName := m.Names[0].Name
						params := make([]string, 0)
						if ft.Params != nil {
							for _, p := range ft.Params.List {
								ptype := exprToString(p.Type)
								if len(p.Names) > 0 {
									for _, n := range p.Names {
										params = append(params, fmt.Sprintf("%s %s", n.Name, ptype))
									}
								} else {
									params = append(params, ptype)
								}
							}
						}

						returns := make([]string, 0)
						if ft.Results != nil {
							for _, r := range ft.Results.List {
								rtype := exprToString(r.Type)
								returns = append(returns, rtype)
							}
						}

						ifInfo.Methods = append(ifInfo.Methods, InterfaceMethod{
							Name:    methodName,
							Params:  params,
							Returns: returns,
							Doc:     cleanDoc(m.Doc),
						})
					}
				}
				symbols.Interfaces = append(symbols.Interfaces, ifInfo)
			}
		}
		return true
	})
}

func extractReceiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return extractReceiverName(t.X)
	case *ast.IndexExpr:
		return extractReceiverName(t.X)
	case *ast.IndexListExpr:
		return extractReceiverName(t.X)
	default:
		return ""
	}
}

func parseStructTags(raw string) map[string]string {
	tags := make(map[string]string)
	st := reflect.StructTag(raw)
	keys := []string{
		"json", "gorm", "db", "validate", "binding", "yaml", "xml",
		"form", "uri", "env", "header", "param", "bson", "dynamodbav", "csv", "redis",
	}
	for _, k := range keys {
		val := st.Get(k)
		if val != "" {
			tags[k] = val
		}
	}
	return tags
}

func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprToString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", exprToString(t.Len), exprToString(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(t.Key), exprToString(t.Value))
	case *ast.InterfaceType:
		return "any"
	case *ast.Ellipsis:
		return "..." + exprToString(t.Elt)
	case *ast.ParenExpr:
		return "(" + exprToString(t.X) + ")"
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", exprToString(t.X), exprToString(t.Index))
	case *ast.IndexListExpr:
		var indices []string
		for _, idx := range t.Indices {
			indices = append(indices, exprToString(idx))
		}
		return fmt.Sprintf("%s[%s]", exprToString(t.X), strings.Join(indices, ", "))
	case *ast.ChanType:
		if t.Dir == ast.RECV {
			return "<-chan " + exprToString(t.Value)
		} else if t.Dir == ast.SEND {
			return "chan<- " + exprToString(t.Value)
		}
		return "chan " + exprToString(t.Value)
	case *ast.FuncType:
		var params []string
		if t.Params != nil {
			for _, p := range t.Params.List {
				params = append(params, exprToString(p.Type))
			}
		}
		var returns []string
		if t.Results != nil {
			for _, r := range t.Results.List {
				returns = append(returns, exprToString(r.Type))
			}
		}
		retStr := strings.Join(returns, ", ")
		if len(returns) > 1 {
			retStr = "(" + retStr + ")"
		}
		if retStr != "" {
			return fmt.Sprintf("func(%s) %s", strings.Join(params, ", "), retStr)
		}
		return fmt.Sprintf("func(%s)", strings.Join(params, ", "))
	case *ast.StructType:
		return "struct{...}"
	default:
		return "unknown"
	}
}

func cleanDoc(docGroup *ast.CommentGroup) string {
	if docGroup == nil {
		return ""
	}
	var lines []string
	for _, c := range docGroup.List {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		lines = append(lines, strings.TrimSpace(text))
	}
	return strings.Join(lines, "\n")
}

func isGeneratedFile(file *ast.File) bool {
	return IsGeneratedAST(file)
}
