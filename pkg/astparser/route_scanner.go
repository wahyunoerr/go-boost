package astparser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
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
	"PATCH": true, "HEAD": true, "OPTIONS": true, "ANY": true, "CONNECT": true, "TRACE": true,
}

var routerTypeHints = []string{
	"gin.Engine", "gin.RouterGroup", "gin.IRouter", "gin.IRoutes",
	"echo.Echo", "echo.Group", "fiber.App", "fiber.Router",
	"chi.Router", "chi.Mux", "mux.Router", "http.ServeMux",
}

func ScanRoutes(rootDir string) ([]RouteInfo, error) {
	files, err := collectGoFiles(rootDir)
	if err != nil {
		return nil, err
	}

	routes := make([]RouteInfo, 0)
	fset := token.NewFileSet()

	type parsedRouteFile struct {
		file *ast.File
		path string
	}
	parsedFiles := make([]parsedRouteFile, 0, len(files))

	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		parsedFiles = append(parsedFiles, parsedRouteFile{file: node, path: path})
	}

	prefixByFunc := map[string]string{}
	for _, pf := range parsedFiles {
		collectRouteRegistrarPrefixes(pf.file, prefixByFunc)
	}

	for _, pf := range parsedFiles {
		routes = append(routes, extractRoutesFromFile(pf.file, fset, relativeTo(rootDir, pf.path), prefixByFunc)...)
	}

	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].File != routes[j].File {
			return routes[i].File < routes[j].File
		}
		return routes[i].Line < routes[j].Line
	})

	return routes, nil
}

func collectRouteRegistrarPrefixes(file *ast.File, out map[string]string) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		groups := resolveGroupVariables(fn.Body, "")

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			calleeName := calleeIdentifier(call.Fun)
			if calleeName == "" {
				return true
			}
			for _, arg := range call.Args {
				ident, ok := arg.(*ast.Ident)
				if !ok {
					continue
				}
				if prefix, exists := groups[ident.Name]; exists && prefix != "" {
					out[calleeName] = prefix
				}
			}
			return true
		})
	}
}

func calleeIdentifier(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func resolveGroupVariables(body *ast.BlockStmt, seed string) map[string]string {
	groups := map[string]string{}

	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Group" && sel.Sel.Name != "Route" && sel.Sel.Name != "PathPrefix") || len(call.Args) == 0 {
				continue
			}
			groupPath, ok := stringLiteral(call.Args[0])
			if !ok {
				continue
			}

			parentPrefix := seed
			if parent, exists := groups[exprToString(sel.X)]; exists {
				parentPrefix = parent
			}

			if i < len(assign.Lhs) {
				if varName := exprToString(assign.Lhs[i]); varName != "" {
					groups[varName] = joinRoutePaths(parentPrefix, groupPath)
				}
			}
		}
		return true
	})

	return groups
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return strings.Trim(lit.Value, "`\""), true
	}
	return value, true
}

func extractRoutesFromFile(file *ast.File, fset *token.FileSet, filePath string, prefixByFunc map[string]string) []RouteInfo {
	var routes []RouteInfo

	detectedFramework := detectFrameworkFromImports(file)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		seed := prefixByFunc[fn.Name.Name]
		groups := resolveGroupVariables(fn.Body, seed)
		routerParams := routerParameterNames(fn.Type)
		if seed != "" {
			for _, param := range routerParams {
				if _, exists := groups[param]; !exists {
					groups[param] = seed
				}
			}
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Route" && sel.Sel.Name != "Group") || len(call.Args) < 2 {
				return true
			}
			groupPath, ok := stringLiteral(call.Args[0])
			if !ok {
				return true
			}
			fnLit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || fnLit.Type == nil || fnLit.Type.Params == nil || len(fnLit.Type.Params.List) == 0 {
				return true
			}
			parentPrefix := groups[exprToString(sel.X)]
			fullPrefix := joinRoutePaths(parentPrefix, groupPath)
			if names := fnLit.Type.Params.List[0].Names; len(names) > 0 {
				groups[names[0].Name] = fullPrefix
			}
			return true
		})

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			if route, ok := routeFromCall(call, sel, groups, routerParams, detectedFramework, fset, filePath); ok {
				routes = append(routes, route)
			}
			return true
		})
	}

	return routes
}

func detectFrameworkFromImports(file *ast.File) string {
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		switch {
		case strings.Contains(p, "gin-gonic/gin"):
			return "gin"
		case strings.Contains(p, "gofiber/fiber"):
			return "fiber"
		case strings.Contains(p, "labstack/echo"):
			return "echo"
		case strings.Contains(p, "go-chi/chi"):
			return "chi"
		case strings.Contains(p, "gorilla/mux"):
			return "gorilla/mux"
		}
	}
	return ""
}

func routerParameterNames(ftype *ast.FuncType) []string {
	var names []string
	if ftype == nil || ftype.Params == nil {
		return names
	}
	for _, p := range ftype.Params.List {
		typeStr := exprToString(p.Type)
		if !looksLikeRouterType(typeStr) {
			continue
		}
		for _, n := range p.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

func looksLikeRouterType(typeStr string) bool {
	clean := strings.TrimPrefix(typeStr, "*")
	for _, hint := range routerTypeHints {
		if strings.Contains(clean, hint) {
			return true
		}
	}
	lower := strings.ToLower(clean)
	return strings.Contains(lower, "router") || strings.Contains(lower, "routergroup") || strings.Contains(lower, "servemux")
}

func routeFromCall(
	call *ast.CallExpr,
	sel *ast.SelectorExpr,
	groups map[string]string,
	routerParams []string,
	detectedFramework string,
	fset *token.FileSet,
	filePath string,
) (RouteInfo, bool) {
	methodName := sel.Sel.Name
	upperMethod := strings.ToUpper(methodName)
	receiverName := exprToString(sel.X)
	line := fset.Position(call.Pos()).Line

	isKnownRouter := isRouterReceiver(receiverName, groups, routerParams)

	if (methodName == "Handle" || methodName == "HandleFunc" || methodName == "Method" || methodName == "MethodFunc") && len(call.Args) >= 3 {
		if verb, ok := stringLiteral(call.Args[0]); ok && httpMethods[strings.ToUpper(verb)] {
			if path, ok := stringLiteral(call.Args[1]); ok {
				return RouteInfo{
					Framework:   frameworkOr(detectedFramework, receiverName),
					Method:      strings.ToUpper(verb),
					Path:        joinRoutePaths(groups[receiverName], path),
					Handler:     handlerName(call.Args[len(call.Args)-1]),
					Middlewares: middlewareNames(call.Args[2 : len(call.Args)-1]),
					File:        filePath,
					Line:        line,
				}, true
			}
		}
	}

	if methodName == "HandleFunc" || methodName == "Handle" {
		if len(call.Args) < 2 {
			return RouteInfo{}, false
		}
		pattern, ok := stringLiteral(call.Args[0])
		if !ok {
			return RouteInfo{}, false
		}

		httpMethod := "ANY"
		pathVal := pattern
		if parts := strings.Fields(pattern); len(parts) == 2 && httpMethods[strings.ToUpper(parts[0])] {
			httpMethod = strings.ToUpper(parts[0])
			pathVal = parts[1]
		}
		if !looksLikeRoutePath(pathVal) && !isKnownRouter {
			return RouteInfo{}, false
		}

		return RouteInfo{
			Framework: frameworkOr(detectedFramework, receiverName),
			Method:    httpMethod,
			Path:      joinRoutePaths(groups[receiverName], pathVal),
			Handler:   handlerName(call.Args[1]),
			File:      filePath,
			Line:      line,
		}, true
	}

	if !httpMethods[upperMethod] || len(call.Args) < 2 {
		return RouteInfo{}, false
	}

	rawPath, ok := stringLiteral(call.Args[0])
	if !ok {
		return RouteInfo{}, false
	}

	if !looksLikeRoutePath(rawPath) && !isKnownRouter {
		return RouteInfo{}, false
	}

	return RouteInfo{
		Framework:   frameworkOr(detectedFramework, receiverName),
		Method:      upperMethod,
		Path:        joinRoutePaths(groups[receiverName], rawPath),
		Handler:     handlerName(call.Args[len(call.Args)-1]),
		Middlewares: middlewareNames(call.Args[1 : len(call.Args)-1]),
		File:        filePath,
		Line:        line,
	}, true
}

func looksLikeRoutePath(path string) bool {
	return strings.HasPrefix(path, "/")
}

func isRouterReceiver(receiver string, groups map[string]string, routerParams []string) bool {
	if _, ok := groups[receiver]; ok {
		return true
	}
	for _, p := range routerParams {
		if p == receiver {
			return true
		}
	}
	return false
}

func handlerName(expr ast.Expr) string {
	if name := exprToString(expr); name != "" && name != "unknown" {
		return name
	}
	if _, ok := expr.(*ast.FuncLit); ok {
		return "func(inline)"
	}
	return "unknown"
}

func middlewareNames(args []ast.Expr) []string {
	middlewares := make([]string, 0, len(args))
	for _, arg := range args {
		if name := exprToString(arg); name != "" && name != "unknown" {
			middlewares = append(middlewares, name)
		}
	}
	if len(middlewares) == 0 {
		return nil
	}
	return middlewares
}

func frameworkOr(detected, receiver string) string {
	if detected != "" {
		return detected
	}
	return inferFramework(receiver)
}

func joinRoutePaths(prefix, subPath string) string {
	p := strings.Trim(prefix, "/")
	s := strings.Trim(subPath, "/")

	switch {
	case p == "" && s == "":
		return "/"
	case p == "":
		return "/" + s
	case s == "":
		return "/" + p
	default:
		return "/" + p + "/" + s
	}
}

func inferFramework(receiver string) string {
	lower := strings.ToLower(receiver)
	switch {
	case strings.Contains(lower, "gin"), lower == "r", lower == "engine", lower == "router":
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
