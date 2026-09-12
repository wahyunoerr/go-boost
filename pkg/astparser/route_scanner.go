package astparser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type RouteInfo struct {
	Framework   string   `json:"framework"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Handler     string   `json:"handler"`
	Middlewares []string `json:"middlewares,omitempty"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
}

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true, "ANY": true,
}

func ScanRoutes(rootDir string) ([]RouteInfo, error) {
	fset := token.NewFileSet()
	routes := make([]RouteInfo, 0)

	err := filepath.Walk(rootDir, func(path string, fi os.FileInfo, err error) error {
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

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}

		fileRoutes := extractRoutesFromFile(node, fset, path)
		routes = append(routes, fileRoutes...)
		return nil
	})

	return routes, err
}

func extractRoutesFromFile(file *ast.File, fset *token.FileSet, filePath string) []RouteInfo {
	var routes []RouteInfo

	detectedFramework := ""
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, "\"")
		if strings.Contains(p, "gin-gonic/gin") {
			detectedFramework = "gin"
		} else if strings.Contains(p, "gofiber/fiber") {
			detectedFramework = "fiber"
		} else if strings.Contains(p, "labstack/echo") {
			detectedFramework = "echo"
		} else if strings.Contains(p, "go-chi/chi") {
			detectedFramework = "chi"
		}
	}

	groupPrefixes := make(map[string]string)

	ast.Inspect(file, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			for i, rhs := range assign.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Group" && len(call.Args) >= 1 {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							groupPath := strings.Trim(lit.Value, "\"")
							parentRecv := exprToString(sel.X)
							parentPrefix := groupPrefixes[parentRecv]
							fullPrefix := joinRoutePaths(parentPrefix, groupPath)

							if i < len(assign.Lhs) {
								varName := exprToString(assign.Lhs[i])
								if varName != "" {
									groupPrefixes[varName] = fullPrefix
								}
							}
						}
					}
				}
			}
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		methodName := sel.Sel.Name
		upperMethod := strings.ToUpper(methodName)
		line := fset.Position(call.Pos()).Line

		if (methodName == "Route" || methodName == "Group") && len(call.Args) >= 2 {
			if pathLit, ok := call.Args[0].(*ast.BasicLit); ok && pathLit.Kind == token.STRING {
				if fnLit, ok := call.Args[1].(*ast.FuncLit); ok {
					groupPath := strings.Trim(pathLit.Value, "\"")
					parentRecv := exprToString(sel.X)
					parentPrefix := groupPrefixes[parentRecv]
					fullPrefix := joinRoutePaths(parentPrefix, groupPath)

					if fnLit.Type != nil && fnLit.Type.Params != nil && len(fnLit.Type.Params.List) > 0 {
						firstParam := fnLit.Type.Params.List[0]
						if len(firstParam.Names) > 0 {
							subVar := firstParam.Names[0].Name
							groupPrefixes[subVar] = fullPrefix
						}
					}
				}
			}
		}

		if httpMethods[upperMethod] && len(call.Args) >= 2 {
			pathLit, ok := call.Args[0].(*ast.BasicLit)
			if ok && pathLit.Kind == token.STRING {
				rawPath := strings.Trim(pathLit.Value, "\"")
				receiverName := exprToString(sel.X)

				fullPath := rawPath
				if prefix, exists := groupPrefixes[receiverName]; exists {
					fullPath = joinRoutePaths(prefix, rawPath)
				}

				handlerName := exprToString(call.Args[len(call.Args)-1])
				middlewares := make([]string, 0)
				if len(call.Args) > 2 {
					for i := 1; i < len(call.Args)-1; i++ {
						middlewares = append(middlewares, exprToString(call.Args[i]))
					}
				}

				fw := detectedFramework
				if fw == "" {
					fw = inferFramework(receiverName)
				}

				routes = append(routes, RouteInfo{
					Framework:   fw,
					Method:      upperMethod,
					Path:        fullPath,
					Handler:     handlerName,
					Middlewares: middlewares,
					File:        filePath,
					Line:        line,
				})
			}
		}

		if (methodName == "HandleFunc" || methodName == "Handle") && len(call.Args) >= 2 {
			pathLit, ok := call.Args[0].(*ast.BasicLit)
			if ok && pathLit.Kind == token.STRING {
				pattern := strings.Trim(pathLit.Value, "\"")
				handlerName := exprToString(call.Args[1])

				httpMethod := "ANY"
				pathVal := pattern

				parts := strings.Fields(pattern)
				if len(parts) == 2 && httpMethods[strings.ToUpper(parts[0])] {
					httpMethod = strings.ToUpper(parts[0])
					pathVal = parts[1]
				}

				routes = append(routes, RouteInfo{
					Framework: "net/http",
					Method:    httpMethod,
					Path:      pathVal,
					Handler:   handlerName,
					File:      filePath,
					Line:      line,
				})
			}
		}

		return true
	})

	return routes
}

func joinRoutePaths(prefix, subPath string) string {
	p := strings.Trim(prefix, "/")
	s := strings.Trim(subPath, "/")
	if p == "" && s == "" {
		return "/"
	}
	if p == "" {
		return "/" + s
	}
	if s == "" {
		return "/" + p
	}
	return "/" + p + "/" + s
}

func inferFramework(receiver string) string {
	lower := strings.ToLower(receiver)
	switch {
	case strings.Contains(lower, "gin"), strings.HasPrefix(lower, "r."), lower == "r", lower == "engine", lower == "router":
		return "gin"
	case strings.Contains(lower, "fiber"), strings.Contains(lower, "app"):
		return "fiber"
	case strings.Contains(lower, "echo"), lower == "e":
		return "echo"
	case strings.Contains(lower, "chi"):
		return "chi"
	default:
		return "net/http"
	}
}
