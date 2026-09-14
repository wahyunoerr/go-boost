package astparser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

type ImplementationMatch struct {
	StructName     string   `json:"struct_name"`
	Package        string   `json:"package,omitempty"`
	StructFile     string   `json:"struct_file"`
	StructLine     int      `json:"struct_line"`
	MatchedMethods []string `json:"matched_methods"`
	MissingMethods []string `json:"missing_methods,omitempty"`
	IsComplete     bool     `json:"is_complete"`
}

type MethodSignature struct {
	Name      string
	Params    []string
	Returns   []string
	IsPointer bool
}

type typeKey struct {
	pkg  string
	name string
}

type typeLocation struct {
	File string
	Line int
}

func FindImplementations(rootDir string, interfaceName string) ([]ImplementationMatch, error) {
	files, err := collectGoFiles(rootDir)
	if err != nil {
		return nil, err
	}

	interfaces := make(map[typeKey][]MethodSignature)
	interfaceEmbeds := make(map[typeKey][]string)
	structMethods := make(map[typeKey][]MethodSignature)
	structEmbeds := make(map[typeKey][]string)
	structLocations := make(map[typeKey]typeLocation)

	fset := token.NewFileSet()

	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}

		pkgName := ""
		if node.Name != nil {
			pkgName = node.Name.Name
		}
		relPath := relativeTo(rootDir, path)

		for _, decl := range node.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, spec := range d.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					key := typeKey{pkg: pkgName, name: typeSpec.Name.Name}

					switch t := typeSpec.Type.(type) {
					case *ast.InterfaceType:
						if t.Methods == nil {
							continue
						}
						for _, m := range t.Methods.List {
							if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) > 0 {
								interfaces[key] = append(interfaces[key], extractFuncSignature(m.Names[0].Name, ft, false))
								continue
							}
							if len(m.Names) == 0 {
								embedded := strings.TrimPrefix(exprToString(m.Type), "*")
								interfaceEmbeds[key] = append(interfaceEmbeds[key], embedded)
							}
						}
						if _, exists := interfaces[key]; !exists {
							interfaces[key] = nil
						}

					case *ast.StructType:
						structLocations[key] = typeLocation{File: relPath, Line: fset.Position(typeSpec.Pos()).Line}
						if t.Fields == nil {
							continue
						}
						for _, f := range t.Fields.List {
							if len(f.Names) == 0 {
								structEmbeds[key] = append(structEmbeds[key], strings.TrimPrefix(exprToString(f.Type), "*"))
							}
						}
					}
				}

			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 {
					continue
				}
				recvName := extractReceiverName(d.Recv.List[0].Type)
				if recvName == "" {
					continue
				}
				_, isPointer := d.Recv.List[0].Type.(*ast.StarExpr)
				key := typeKey{pkg: pkgName, name: recvName}
				structMethods[key] = append(structMethods[key], extractFuncSignature(d.Name.Name, d.Type, isPointer))
			}
		}
	}

	targetKey, found := resolveInterfaceKey(interfaces, interfaceEmbeds, interfaceName)
	if !found {
		return nil, fmt.Errorf("interface %s not found in codebase", interfaceName)
	}

	targetMethods, _ := resolveInterfaceMethods(targetKey, interfaces, interfaceEmbeds, map[typeKey]bool{})
	if len(targetMethods) == 0 {
		return nil, fmt.Errorf("interface %s declares no methods that can be matched", interfaceName)
	}

	promoteEmbeddedMethods(structMethods, structEmbeds)

	matches := make([]ImplementationMatch, 0)
	for key, loc := range structLocations {
		methods := structMethods[key]
		matched := make([]string, 0)
		missing := make([]string, 0)

		for _, ifMethod := range targetMethods {
			if hasMatchingMethod(methods, ifMethod) {
				matched = append(matched, ifMethod.Name)
			} else {
				missing = append(missing, ifMethod.Name)
			}
		}

		if len(matched) == 0 {
			continue
		}

		matches = append(matches, ImplementationMatch{
			StructName:     key.name,
			Package:        key.pkg,
			StructFile:     loc.File,
			StructLine:     loc.Line,
			MatchedMethods: matched,
			MissingMethods: missing,
			IsComplete:     len(missing) == 0,
		})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].IsComplete != matches[j].IsComplete {
			return matches[i].IsComplete
		}
		if matches[i].Package != matches[j].Package {
			return matches[i].Package < matches[j].Package
		}
		return matches[i].StructName < matches[j].StructName
	})

	return matches, nil
}

func resolveInterfaceKey(interfaces map[typeKey][]MethodSignature, embeds map[typeKey][]string, name string) (typeKey, bool) {
	pkg, plain := splitQualifiedName(name)

	candidates := make([]typeKey, 0)
	for key := range interfaces {
		if key.name == plain && (pkg == "" || key.pkg == pkg) {
			candidates = append(candidates, key)
		}
	}
	for key := range embeds {
		if key.name != plain || (pkg != "" && key.pkg != pkg) {
			continue
		}
		if _, exists := interfaces[key]; !exists {
			candidates = append(candidates, key)
		}
	}

	if len(candidates) == 0 {
		return typeKey{}, false
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].pkg < candidates[j].pkg })
	return candidates[0], true
}

func splitQualifiedName(name string) (string, string) {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return "", name
}

func resolveInterfaceMethods(key typeKey, interfaces map[typeKey][]MethodSignature, embeds map[typeKey][]string, visited map[typeKey]bool) ([]MethodSignature, bool) {
	if visited[key] {
		return nil, false
	}
	visited[key] = true

	methods, exists := interfaces[key]
	embedded := embeds[key]
	if !exists && len(embedded) == 0 {
		return nil, false
	}

	result := append([]MethodSignature{}, methods...)
	for _, name := range embedded {
		pkg, plain := splitQualifiedName(name)
		if pkg == "" {
			pkg = key.pkg
		}
		sub, ok := resolveInterfaceMethods(typeKey{pkg: pkg, name: plain}, interfaces, embeds, visited)
		if ok {
			result = append(result, sub...)
		}
	}

	return result, true
}

func promoteEmbeddedMethods(structMethods map[typeKey][]MethodSignature, structEmbeds map[typeKey][]string) {
	for depth := 0; depth < 4; depth++ {
		changed := false
		for key, embeds := range structEmbeds {
			for _, embed := range embeds {
				pkg, plain := splitQualifiedName(embed)
				if pkg == "" {
					pkg = key.pkg
				}
				base, ok := structMethods[typeKey{pkg: pkg, name: plain}]
				if !ok {
					continue
				}
				for _, method := range base {
					if !hasMethodNamed(structMethods[key], method.Name) {
						structMethods[key] = append(structMethods[key], method)
						changed = true
					}
				}
			}
		}
		if !changed {
			return
		}
	}
}

func hasMethodNamed(methods []MethodSignature, name string) bool {
	for _, m := range methods {
		if m.Name == name {
			return true
		}
	}
	return false
}

func hasMatchingMethod(methods []MethodSignature, target MethodSignature) bool {
	for _, m := range methods {
		if matchSignatures(m, target) {
			return true
		}
	}
	return false
}

func matchSignatures(s, i MethodSignature) bool {
	if s.Name != i.Name {
		return false
	}
	if len(s.Params) != len(i.Params) || len(s.Returns) != len(i.Returns) {
		return false
	}
	for idx := range s.Params {
		if !typesMatch(s.Params[idx], i.Params[idx]) {
			return false
		}
	}
	for idx := range s.Returns {
		if !typesMatch(s.Returns[idx], i.Returns[idx]) {
			return false
		}
	}
	return true
}

func typesMatch(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return true
	}

	aPointer := strings.HasPrefix(a, "*")
	bPointer := strings.HasPrefix(b, "*")
	if aPointer != bPointer {
		return false
	}

	aPlain := strings.TrimPrefix(a, "*")
	bPlain := strings.TrimPrefix(b, "*")
	if aPlain == bPlain {
		return true
	}

	return strings.HasSuffix(aPlain, "."+bPlain) || strings.HasSuffix(bPlain, "."+aPlain)
}

func extractFuncSignature(name string, ft *ast.FuncType, isPointer bool) MethodSignature {
	sig := MethodSignature{Name: name, IsPointer: isPointer}

	if ft.Params != nil {
		for _, p := range ft.Params.List {
			ptype := exprToString(p.Type)
			count := len(p.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				sig.Params = append(sig.Params, ptype)
			}
		}
	}

	if ft.Results != nil {
		for _, r := range ft.Results.List {
			rtype := exprToString(r.Type)
			count := len(r.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				sig.Returns = append(sig.Returns, rtype)
			}
		}
	}

	return sig
}

func extractReceiverTypeName(expr ast.Expr) string {
	return extractReceiverName(expr)
}
