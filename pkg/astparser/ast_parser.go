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
	"sort"
	"strings"
	"sync"
)

type StructInfo struct {
	Name       string      `json:"name"`
	Package    string      `json:"package,omitempty"`
	Doc        string      `json:"doc,omitempty"`
	File       string      `json:"file"`
	Line       int         `json:"line"`
	TypeParams []string    `json:"type_params,omitempty"`
	Fields     []FieldInfo `json:"fields"`
	Methods    []string    `json:"methods,omitempty"`
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
	Name       string            `json:"name"`
	Package    string            `json:"package,omitempty"`
	Doc        string            `json:"doc,omitempty"`
	File       string            `json:"file"`
	Line       int               `json:"line"`
	TypeParams []string          `json:"type_params,omitempty"`
	Embeds     []string          `json:"embeds,omitempty"`
	Methods    []InterfaceMethod `json:"methods"`
}

type InterfaceMethod struct {
	Name    string   `json:"name"`
	Params  []string `json:"params"`
	Returns []string `json:"returns"`
	Doc     string   `json:"doc,omitempty"`

	ParamNames []string `json:"param_names,omitempty"`
	ParamTypes []string `json:"param_types,omitempty"`
	Variadic   bool     `json:"variadic,omitempty"`
}

type PackageSymbols struct {
	Package    string          `json:"package"`
	Structs    []StructInfo    `json:"structs"`
	Interfaces []InterfaceInfo `json:"interfaces"`
}

func ParsePath(targetPath string) (*PackageSymbols, error) {
	symbols := &PackageSymbols{
		Structs:    make([]StructInfo, 0),
		Interfaces: make([]InterfaceInfo, 0),
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, fmt.Errorf("stat path error: %w", err)
	}

	if !info.IsDir() {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, targetPath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse file error: %w", err)
		}
		symbols.Package = node.Name.Name
		inspectFile(node, fset, targetPath, symbols)
		attachMethods(symbols, collectMethods([]*parsedFile{{file: node, fset: fset, path: targetPath}}))
		sortSymbols(symbols)
		return symbols, nil
	}

	files, err := collectGoFiles(targetPath)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return symbols, nil
	}

	parsed := parseFilesConcurrently(files)

	for _, pf := range parsed {
		if symbols.Package == "" && pf.file.Name != nil {
			symbols.Package = pf.file.Name.Name
		}
		inspectFile(pf.file, pf.fset, relativeTo(targetPath, pf.path), symbols)
	}

	attachMethods(symbols, collectMethods(parsed))
	sortSymbols(symbols)

	return symbols, nil
}

type parsedFile struct {
	file *ast.File
	fset *token.FileSet
	path string
}

func collectGoFiles(targetPath string) ([]string, error) {
	return walkGoFiles(targetPath, false)
}

func collectGoFilesWithTests(targetPath string) ([]string, error) {
	return walkGoFiles(targetPath, true)
}

func walkGoFiles(targetPath string, includeTests bool) ([]string, error) {
	var files []string
	err := filepath.Walk(targetPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			if path != targetPath && ShouldSkipDir(fi) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipFile(path, includeTests) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func parseFilesConcurrently(files []string) []*parsedFile {
	numWorkers := runtime.NumCPU()
	if numWorkers > len(files) {
		numWorkers = len(files)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	results := make([]*parsedFile, len(files))
	var wg sync.WaitGroup
	indexes := make(chan int, len(files))
	for i := range files {
		indexes <- i
	}
	close(indexes)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range indexes {
				fset := token.NewFileSet()
				node, err := parser.ParseFile(fset, files[idx], nil, parser.ParseComments)
				if err != nil || IsGeneratedAST(node) {
					continue
				}
				results[idx] = &parsedFile{file: node, fset: fset, path: files[idx]}
			}
		}()
	}
	wg.Wait()

	parsed := make([]*parsedFile, 0, len(files))
	for _, pf := range results {
		if pf != nil {
			parsed = append(parsed, pf)
		}
	}
	return parsed
}

type methodKey struct {
	pkg  string
	name string
}

func collectMethods(parsed []*parsedFile) map[methodKey][]string {
	methods := make(map[methodKey][]string)

	for _, pf := range parsed {
		pkgName := ""
		if pf.file.Name != nil {
			pkgName = pf.file.Name.Name
		}
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recvName := extractReceiverName(fn.Recv.List[0].Type)
			if recvName == "" {
				continue
			}
			key := methodKey{pkg: pkgName, name: recvName}
			methods[key] = append(methods[key], fn.Name.Name)
		}
	}

	for key := range methods {
		sort.Strings(methods[key])
	}
	return methods
}

func attachMethods(symbols *PackageSymbols, methods map[methodKey][]string) {
	for i := range symbols.Structs {
		key := methodKey{pkg: symbols.Structs[i].Package, name: symbols.Structs[i].Name}
		if m, ok := methods[key]; ok {
			symbols.Structs[i].Methods = m
		}
	}
}

func sortSymbols(symbols *PackageSymbols) {
	sort.SliceStable(symbols.Structs, func(i, j int) bool {
		a, b := symbols.Structs[i], symbols.Structs[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.File < b.File
	})
	sort.SliceStable(symbols.Interfaces, func(i, j int) bool {
		a, b := symbols.Interfaces[i], symbols.Interfaces[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.File < b.File
	})
}

func inspectFile(file *ast.File, fset *token.FileSet, filePath string, symbols *PackageSymbols) {
	pkgName := ""
	if file.Name != nil {
		pkgName = file.Name.Name
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			line := fset.Position(typeSpec.Pos()).Line
			doc := cleanDoc(genDecl.Doc)
			if typeSpec.Doc != nil {
				doc = cleanDoc(typeSpec.Doc)
			}

			switch t := typeSpec.Type.(type) {
			case *ast.StructType:
				symbols.Structs = append(symbols.Structs, StructInfo{
					Name:       typeSpec.Name.Name,
					Package:    pkgName,
					Doc:        doc,
					File:       filePath,
					Line:       line,
					TypeParams: extractTypeParams(typeSpec.TypeParams),
					Fields:     extractFields(t.Fields),
				})

			case *ast.InterfaceType:
				iface := InterfaceInfo{
					Name:       typeSpec.Name.Name,
					Package:    pkgName,
					Doc:        doc,
					File:       filePath,
					Line:       line,
					TypeParams: extractTypeParams(typeSpec.TypeParams),
					Methods:    make([]InterfaceMethod, 0),
				}

				if t.Methods != nil {
					for _, m := range t.Methods.List {
						ft, isFunc := m.Type.(*ast.FuncType)
						if !isFunc || len(m.Names) == 0 {
							if name := strings.TrimPrefix(exprToString(m.Type), "*"); name != "" && name != "unknown" {
								iface.Embeds = append(iface.Embeds, name)
							}
							continue
						}
						iface.Methods = append(iface.Methods, buildInterfaceMethod(m.Names[0].Name, ft, cleanDoc(m.Doc)))
					}
				}

				symbols.Interfaces = append(symbols.Interfaces, iface)
			}
		}
	}
}

func extractTypeParams(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var params []string
	for _, f := range fields.List {
		constraint := exprToString(f.Type)
		for _, name := range f.Names {
			params = append(params, name.Name+" "+constraint)
		}
	}
	return params
}

func extractFields(fields *ast.FieldList) []FieldInfo {
	result := make([]FieldInfo, 0)
	if fields == nil {
		return result
	}

	for _, field := range fields.List {
		typeStr := exprToString(field.Type)

		rawTag := ""
		tagMap := map[string]string{}
		if field.Tag != nil {
			rawTag = strings.Trim(field.Tag.Value, "`")
			tagMap = parseStructTags(rawTag)
		}

		if len(field.Names) == 0 {
			result = append(result, FieldInfo{
				Name:       strings.TrimPrefix(typeStr, "*"),
				Type:       typeStr,
				Tag:        rawTag,
				ParsedTags: tagMap,
				Doc:        cleanDoc(field.Doc),
				Embedded:   true,
			})
			continue
		}

		for _, name := range field.Names {
			result = append(result, FieldInfo{
				Name:       name.Name,
				Type:       typeStr,
				Tag:        rawTag,
				ParsedTags: tagMap,
				Doc:        cleanDoc(field.Doc),
			})
		}
	}

	return result
}

func buildInterfaceMethod(name string, ft *ast.FuncType, doc string) InterfaceMethod {
	method := InterfaceMethod{
		Name:       name,
		Doc:        doc,
		Params:     make([]string, 0),
		Returns:    make([]string, 0),
		ParamNames: make([]string, 0),
		ParamTypes: make([]string, 0),
	}

	if ft.Params != nil {
		for _, p := range ft.Params.List {
			typeStr := exprToString(p.Type)
			if _, isEllipsis := p.Type.(*ast.Ellipsis); isEllipsis {
				method.Variadic = true
			}

			if len(p.Names) == 0 {
				method.Params = append(method.Params, typeStr)
				method.ParamTypes = append(method.ParamTypes, typeStr)
				method.ParamNames = append(method.ParamNames, fmt.Sprintf("arg%d", len(method.ParamNames)))
				continue
			}
			for _, n := range p.Names {
				method.Params = append(method.Params, fmt.Sprintf("%s %s", n.Name, typeStr))
				method.ParamTypes = append(method.ParamTypes, typeStr)
				method.ParamNames = append(method.ParamNames, n.Name)
			}
		}
	}

	if ft.Results != nil {
		for _, r := range ft.Results.List {
			typeStr := exprToString(r.Type)
			count := len(r.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				method.Returns = append(method.Returns, typeStr)
			}
		}
	}

	return method
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
		if val := st.Get(k); val != "" {
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
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "any"
		}
		return "interface{...}"
	case *ast.Ellipsis:
		return "..." + exprToString(t.Elt)
	case *ast.ParenExpr:
		return "(" + exprToString(t.X) + ")"
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", exprToString(t.X), exprToString(t.Index))
	case *ast.IndexListExpr:
		indices := make([]string, 0, len(t.Indices))
		for _, idx := range t.Indices {
			indices = append(indices, exprToString(idx))
		}
		return fmt.Sprintf("%s[%s]", exprToString(t.X), strings.Join(indices, ", "))
	case *ast.ChanType:
		switch t.Dir {
		case ast.RECV:
			return "<-chan " + exprToString(t.Value)
		case ast.SEND:
			return "chan<- " + exprToString(t.Value)
		default:
			return "chan " + exprToString(t.Value)
		}
	case *ast.FuncType:
		return funcTypeToString(t)
	case *ast.StructType:
		return "struct{...}"
	case *ast.BasicLit:
		return t.Value
	case *ast.BinaryExpr:
		return exprToString(t.X) + " " + t.Op.String() + " " + exprToString(t.Y)
	case *ast.UnaryExpr:
		return t.Op.String() + exprToString(t.X)
	default:
		return "unknown"
	}
}

func funcTypeToString(t *ast.FuncType) string {
	var params []string
	if t.Params != nil {
		for _, p := range t.Params.List {
			typeStr := exprToString(p.Type)
			count := len(p.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				params = append(params, typeStr)
			}
		}
	}

	var returns []string
	if t.Results != nil {
		for _, r := range t.Results.List {
			typeStr := exprToString(r.Type)
			count := len(r.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				returns = append(returns, typeStr)
			}
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
