package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
	"github.com/wahyunoerr/go-boost/pkg/database"
	"github.com/wahyunoerr/go-boost/pkg/detector"
	"github.com/wahyunoerr/go-boost/pkg/generator"
	"github.com/wahyunoerr/go-boost/pkg/logs"
	"github.com/wahyunoerr/go-boost/pkg/runner"
	"github.com/wahyunoerr/go-boost/pkg/security"
)

func RegisterAllTools(s *Server, rootDir string) {
	s.RegisterTool(Tool{
		Name:        "app_info",
		Description: "Get comprehensive Go application information including Go version, module name, detected web framework, ORM, database engine, logger, architecture pattern, and all installed packages with exact versions. Always call this tool when starting work on a Go project.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		d := detector.NewDetector(rootDir)
		stack, err := d.Detect()
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(stack, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "ast_inspect",
		Description: "Deep static AST inspection of Go files or packages. Extracts all structs, interfaces, methods, comments, and field tags (json, gorm, db, validate) without executing code. Highly useful for understanding data models and contracts.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"path": {
					Type:        "string",
					Description: "Relative or absolute directory or file path to inspect (e.g. './internal/domain' or './models/user.go'). Defaults to root directory.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		target := rootDir
		if p, ok := args["path"].(string); ok && p != "" {
			if filepath.IsAbs(p) {
				target = p
			} else {
				target = filepath.Join(rootDir, p)
			}
		}
		symbols, err := astparser.ParsePath(target)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(symbols, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "route_list",
		Description: "Scan Go codebase AST for all registered HTTP routes across Gin, Echo, Fiber, Chi, and Go 1.22+ net/http pattern routes. Returns methods, path patterns, handlers, and source locations.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		routes, err := astparser.ScanRoutes(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(routes, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "db_connections",
		Description: "List configured database connection names and parameters detected from .env and config files.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		conns, err := database.DiscoverConnections(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(conns, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "db_schema",
		Description: "Inspect the database schema. Returns table names, columns, indexes, and foreign keys. Use 'summary: true' first to get a quick overview of tables and column types, then call again with 'filter' for full details on specific tables.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"summary": {
					Type:        "boolean",
					Description: "Return only table names and column types. Defaults to false.",
				},
				"filter": {
					Type:        "string",
					Description: "Filter tables by name (substring match).",
				},
				"connection": {
					Type:        "string",
					Description: "Specific connection name to use (defaults to first discovered connection).",
				},
				"include_views": {
					Type:        "boolean",
					Description: "Include database views in output.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		conns, err := database.DiscoverConnections(rootDir)
		if err != nil {
			return nil, err
		}
		if len(conns.Connections) == 0 {
			return &CallToolResult{
				Content: []Content{NewTextContent("No database connection configured or found.")},
			}, nil
		}

		var targetConn *database.DBConnection
		if connName, ok := args["connection"].(string); ok && connName != "" {
			for i := range conns.Connections {
				if conns.Connections[i].Name == connName {
					targetConn = &conns.Connections[i]
					break
				}
			}
		}
		if targetConn == nil {
			targetConn = &conns.Connections[0]
		}

		opts := database.SchemaOptions{}
		if s, ok := args["summary"].(bool); ok {
			opts.Summary = s
		}
		if f, ok := args["filter"].(string); ok {
			opts.Filter = f
		}
		if v, ok := args["include_views"].(bool); ok {
			opts.IncludeViews = v
		}

		schema, err := database.Introspect(ctx, targetConn, opts)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(schema, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "db_query",
		Description: "Execute a strictly read-only SQL query against the configured database (e.g. SELECT, SHOW, EXPLAIN, DESCRIBE, PRAGMA). Mutations like INSERT, UPDATE, DELETE, and DROP are strictly blocked.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"query": {
					Type:        "string",
					Description: "The read-only SQL query to execute.",
				},
				"connection": {
					Type:        "string",
					Description: "Optional database connection name to target.",
				},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return nil, fmt.Errorf("parameter 'query' is required")
		}

		conns, err := database.DiscoverConnections(rootDir)
		if err != nil {
			return nil, err
		}

		var targetConn *database.DBConnection
		if len(conns.Connections) > 0 {
			targetConn = &conns.Connections[0]
			if connName, ok := args["connection"].(string); ok && connName != "" {
				for i := range conns.Connections {
					if conns.Connections[i].Name == connName {
						targetConn = &conns.Connections[i]
						break
					}
				}
			}
		}

		out, err := database.ExecuteQuery(ctx, targetConn, query)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []Content{NewTextContent(out)},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "read_logs",
		Description: "Read the last N log entries from application log files, automatically parsing slog JSON, zap JSON, zerolog, and standard multi-line logs.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"entries": {
					Type:        "integer",
					Description: "Number of entries to return (default 50).",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		maxEntries := 50
		if num, ok := args["entries"].(float64); ok && num > 0 {
			maxEntries = int(num)
		}
		entries, err := logs.ReadEntries(rootDir, maxEntries)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "last_error",
		Description: "Get details of the last backend error, exception, or panic in this application, including file, line number, and stack trace.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		lastErr, err := logs.FindLastError(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(lastErr, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "test_runner",
		Description: "Run Go unit tests with targeted package, test name filter (-run), and coverage reporting. Returns structured results showing exact failure points and error messages without passing test noise.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"package": {
					Type:        "string",
					Description: "Package path to test (e.g. './pkg/detector/...' or './...'). Defaults to './...'.",
				},
				"run": {
					Type:        "string",
					Description: "Regex pattern for specific tests to run (e.g. 'TestUser').",
				},
				"coverage": {
					Type:        "boolean",
					Description: "Calculate test coverage percentage.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		opts := runner.TestOptions{}
		if p, ok := args["package"].(string); ok {
			opts.Package = p
		}
		if r, ok := args["run"].(string); ok {
			opts.Run = r
		}
		if c, ok := args["coverage"].(bool); ok {
			opts.Coverage = c
		}
		res, err := runner.RunTests(ctx, rootDir, opts)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "go_doc",
		Description: "Search and retrieve official documentation and function/struct signatures for Go standard library symbols or imported project packages via 'go doc'.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"symbol": {
					Type:        "string",
					Description: "Symbol or package to document (e.g. 'net/http.Client', 'fmt.Sprintf', or 'gorm.io/gorm.DB').",
				},
			},
			Required: []string{"symbol"},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		sym, ok := args["symbol"].(string)
		if !ok || sym == "" {
			return nil, fmt.Errorf("parameter 'symbol' is required")
		}
		doc, err := runner.LookupDoc(ctx, rootDir, sym)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []Content{NewTextContent(doc)},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "code_check",
		Description: "Run type checking and 'go vet' on Go packages to immediately verify code correctness and diagnose syntax or static analysis issues.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"package": {
					Type:        "string",
					Description: "Package path to check (e.g. './...'). Defaults to './...'.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		pkg := "./..."
		if p, ok := args["package"].(string); ok && p != "" {
			pkg = p
		}
		diag, err := runner.CodeCheck(ctx, rootDir, pkg)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []Content{NewTextContent(diag)},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "find_implementations",
		Description: "Statically find which Go structs in the codebase implement a given interface by computing AST method sets.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"interface": {
					Type:        "string",
					Description: "The interface name to inspect (e.g. 'UserRepository' or 'PaymentGateway').",
				},
			},
			Required: []string{"interface"},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		iface, ok := args["interface"].(string)
		if !ok || iface == "" {
			return nil, fmt.Errorf("parameter 'interface' is required")
		}
		matches, err := astparser.FindImplementations(rootDir, iface)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(matches, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "concurrency_check",
		Description: "Perform static AST analysis to identify Go concurrency hazards: context timer leaks (missing defer cancel), mutex deadlocks (missing defer unlock), and goroutine risks.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		issues, err := astparser.CheckConcurrency(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(issues, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "schema_struct_diff",
		Description: "Compare database table columns against Go struct field tags (db, gorm, json) to detect missing columns, typos, and schema synchronization discrepancies.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"connection": {
					Type:        "string",
					Description: "Optional database connection name to target.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		conns, err := database.DiscoverConnections(rootDir)
		if err != nil || len(conns.Connections) == 0 {
			return &CallToolResult{
				Content: []Content{NewTextContent("No database connection configured.")},
			}, nil
		}
		var targetConn = &conns.Connections[0]
		if connName, ok := args["connection"].(string); ok && connName != "" {
			for i := range conns.Connections {
				if conns.Connections[i].Name == connName {
					targetConn = &conns.Connections[i]
					break
				}
			}
		}

		diffs, err := database.DiffSchemaAndStructs(ctx, targetConn, rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(diffs, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "bench_runner",
		Description: "Execute Go benchmarks with memory allocation profiling (-benchmem) and parse ns/op, B/op, and allocs/op metrics.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"package": {
					Type:        "string",
					Description: "Package path to benchmark (defaults to './...').",
				},
				"filter": {
					Type:        "string",
					Description: "Benchmark regex filter (defaults to '.').",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		pkg := "./..."
		if p, ok := args["package"].(string); ok && p != "" {
			pkg = p
		}
		filter := "."
		if f, ok := args["filter"].(string); ok && f != "" {
			filter = f
		}

		report, err := runner.RunBenchmark(ctx, rootDir, pkg, filter)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "security_scan",
		Description: "Perform static AST security analysis to detect SQL injection vulnerabilities, hardcoded secrets/tokens, and insecure TLS configurations.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		vulns, err := security.ScanCodebase(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(vulns, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "mock_generate",
		Description: "Generate a pure-Go mock struct for any interface in the codebase to use in unit tests.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"interface": {
					Type:        "string",
					Description: "Name of the interface to generate mock code for (e.g. 'UserRepository').",
				},
			},
			Required: []string{"interface"},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		iface, ok := args["interface"].(string)
		if !ok || iface == "" {
			return nil, fmt.Errorf("parameter 'interface' is required")
		}
		mockCode, err := generator.GenerateMockCode(rootDir, iface)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []Content{NewTextContent(mockCode)},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "openapi_generate",
		Description: "Automatically generate an OpenAPI 3.0 specification JSON document directly from static AST HTTP route discovery.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"title": {
					Type:        "string",
					Description: "API document title (e.g. 'My Service API').",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		title := "Go API"
		if t, ok := args["title"].(string); ok && t != "" {
			title = t
		}
		spec, err := generator.GenerateOpenAPISpec(rootDir, title)
		if err != nil {
			return nil, err
		}
		return &CallToolResult{
			Content: []Content{NewTextContent(spec)},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "migration_generate",
		Description: "Automatically generate SQL up/down migration files based on differences between database tables and Go struct field tags.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"connection": {
					Type:        "string",
					Description: "Optional database connection name.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		conns, err := database.DiscoverConnections(rootDir)
		if err != nil || len(conns.Connections) == 0 {
			return &CallToolResult{
				Content: []Content{NewTextContent("No database connection available.")},
			}, nil
		}
		var targetConn = &conns.Connections[0]
		if connName, ok := args["connection"].(string); ok && connName != "" {
			for i := range conns.Connections {
				if conns.Connections[i].Name == connName {
					targetConn = &conns.Connections[i]
					break
				}
			}
		}

		files, err := database.GenerateMigrationScaffold(ctx, targetConn, rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(files, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})

	s.RegisterTool(Tool{
		Name:        "deadcode_detect",
		Description: "Statically scan the codebase AST to detect unused structs, functions, or interfaces.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		unused, err := astparser.DetectDeadCode(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(unused, "", "  ")
		return &CallToolResult{
			Content: []Content{NewTextContent(string(data))},
		}, nil
	})
}
