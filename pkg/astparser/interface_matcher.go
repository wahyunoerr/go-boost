package astparser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type ImplementationMatch struct {
	StructName     string   `json:"struct_name"`
	StructFile     string   `json:"struct_file"`
	StructLine     int      `json:"struct_line"`
	MatchedMethods []string `json:"matched_methods"`
	MissingMethods []string `json:"missing_methods,omitempty"`
	IsComplete     bool     `json:"is_complete"`
}

type MethodSignature struct {
	Name    string
	Params  []string
	Returns []string
}

func FindImplementations(rootDir string, interfaceName string) ([]ImplementationMatch, error) {
	fset := token.NewFileSet()

	allInterfaces := make(map[string][]MethodSignature)
	interfaceEmbeds := make(map[string][]string)

	structMethods := make(map[string][]MethodSignature)
	structEmbeds := make(map[string][]string)
	structLocations := make(map[string]struct {
		File string
		Line int
	})

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			if fi != nil && (strings.HasPrefix(fi.Name(), ".") || fi.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		for _, decl := range node.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					for _, spec := range d.Specs {
						typeSpec, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}

						if ifType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							iName := typeSpec.Name.Name
							if ifType.Methods != nil {
								for _, m := range ifType.Methods.List {
									if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) > 0 {
										allInterfaces[iName] = append(allInterfaces[iName], extractFuncSignature(m.Names[0].Name, ft))
									} else if len(m.Names) == 0 {
										embeddedName := strings.TrimPrefix(exprToString(m.Type), "*")
										interfaceEmbeds[iName] = append(interfaceEmbeds[iName], embeddedName)
									}
								}
							}
						}

						if stType, ok := typeSpec.Type.(*ast.StructType); ok {
							sName := typeSpec.Name.Name
							pos := fset.Position(typeSpec.Pos())
							structLocations[sName] = struct {
								File string
								Line int
							}{
								File: path,
								Line: pos.Line,
							}

							if stType.Fields != nil {
								for _, f := range stType.Fields.List {
									if len(f.Names) == 0 {
										emb := strings.TrimPrefix(exprToString(f.Type), "*")
										structEmbeds[sName] = append(structEmbeds[sName], emb)
									}
								}
							}
						}
					}
				}

			case *ast.FuncDecl:

				if d.Recv != nil && len(d.Recv.List) > 0 {
					recvTypeName := extractReceiverTypeName(d.Recv.List[0].Type)
					if recvTypeName != "" {
						sig := extractFuncSignature(d.Name.Name, d.Type)
						structMethods[recvTypeName] = append(structMethods[recvTypeName], sig)
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	targetInterfaceMethods, foundInterface := resolveInterfaceMethods(interfaceName, allInterfaces, interfaceEmbeds, make(map[string]bool))
	if !foundInterface {
		return nil, fmt.Errorf("interface %s not found in codebase", interfaceName)
	}

	for sName, embeds := range structEmbeds {
		for _, emb := range embeds {
			if baseMethods, ok := structMethods[emb]; ok {
				structMethods[sName] = append(structMethods[sName], baseMethods...)
			}
		}
	}

	matches := make([]ImplementationMatch, 0)

	for structName, loc := range structLocations {
		methods := structMethods[structName]
		matched := make([]string, 0)
		missing := make([]string, 0)

		for _, ifMethod := range targetInterfaceMethods {
			var hasMethod bool
			for _, stMethod := range methods {
				if matchSignatures(stMethod, ifMethod) {
					hasMethod = true
					break
				}
			}

			if hasMethod {
				matched = append(matched, ifMethod.Name)
			} else {
				missing = append(missing, ifMethod.Name)
			}
		}

		if len(matched) > 0 {
			matches = append(matches, ImplementationMatch{
				StructName:     structName,
				StructFile:     loc.File,
				StructLine:     loc.Line,
				MatchedMethods: matched,
				MissingMethods: missing,
				IsComplete:     len(missing) == 0,
			})
		}
	}

	return matches, nil
}

func resolveInterfaceMethods(name string, allInterfaces map[string][]MethodSignature, interfaceEmbeds map[string][]string, visited map[string]bool) ([]MethodSignature, bool) {
	if visited[name] {
		return nil, false
	}
	visited[name] = true

	methods, exists := allInterfaces[name]
	embeds := interfaceEmbeds[name]
	if !exists && len(embeds) == 0 {
		return nil, false
	}

	result := append([]MethodSignature{}, methods...)
	for _, emb := range embeds {
		subMethods, found := resolveInterfaceMethods(emb, allInterfaces, interfaceEmbeds, visited)
		if found {
			result = append(result, subMethods...)
		}
	}

	return result, true
}

func matchSignatures(s, i MethodSignature) bool {
	if s.Name != i.Name {
		return false
	}
	if len(s.Params) != len(i.Params) || len(s.Returns) != len(i.Returns) {
		return false
	}
	for idx := range s.Params {
		sp := normalizeType(s.Params[idx])
		ip := normalizeType(i.Params[idx])
		if sp != ip && !strings.HasSuffix(sp, "."+ip) && !strings.HasSuffix(ip, "."+sp) {
			return false
		}
	}
	for idx := range s.Returns {
		sr := normalizeType(s.Returns[idx])
		ir := normalizeType(i.Returns[idx])
		if sr != ir && !strings.HasSuffix(sr, "."+ir) && !strings.HasSuffix(ir, "."+sr) {
			return false
		}
	}
	return true
}

func normalizeType(t string) string {
	return strings.TrimSpace(strings.TrimPrefix(t, "*"))
}

func extractFuncSignature(name string, ft *ast.FuncType) MethodSignature {
	sig := MethodSignature{Name: name}

	if ft.Params != nil {
		for _, p := range ft.Params.List {
			ptype := exprToString(p.Type)
			if len(p.Names) > 0 {
				for range p.Names {
					sig.Params = append(sig.Params, ptype)
				}
			} else {
				sig.Params = append(sig.Params, ptype)
			}
		}
	}

	if ft.Results != nil {
		for _, r := range ft.Results.List {
			rtype := exprToString(r.Type)
			if len(r.Names) > 0 {
				for range r.Names {
					sig.Returns = append(sig.Returns, rtype)
				}
			} else {
				sig.Returns = append(sig.Returns, rtype)
			}
		}
	}

	return sig
}

func extractReceiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return extractReceiverTypeName(t.X)
	case *ast.IndexExpr:
		return extractReceiverTypeName(t.X)
	default:
		return ""
	}
}
