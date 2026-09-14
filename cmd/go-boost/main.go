package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
	"github.com/wahyunoerr/go-boost/pkg/database"
	"github.com/wahyunoerr/go-boost/pkg/detector"
	"github.com/wahyunoerr/go-boost/pkg/generator"
	"github.com/wahyunoerr/go-boost/pkg/mcp"
	"github.com/wahyunoerr/go-boost/pkg/runner"
	"github.com/wahyunoerr/go-boost/pkg/security"
)

const (
	Version = "2.0.0"
	Banner  = `
   ____ _       ____                  _   
  / ___/___    | __ )  ___   ___  ___| |_ 
 | |  _/ _ \---|  _ \ / _ \ / _ \/ __| __|
 | |_| | (_) |---| |_) | (_) | (_) \__ \ |_ 
  \____|\___/  |____/ \___/ \___/|___/\__|
  The AI Acceleration & MCP Suite for Go (v` + Version + `)
`
)

const (
	exitOK       = 0
	exitError    = 1
	exitFindings = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		printHelp()
		return exitOK
	}

	switch os.Args[1] {
	case "run":
		return runSupervisedApp()
	case "mcp":
		return runMCPServer()
	case "init", "install":
		return runInit()
	case "status":
		return runStatus()
	case "routes":
		return runRoutes()
	case "schema":
		return runSchema()
	case "inspect":
		return runInspect()
	case "check":
		return runCheck()
	case "bench":
		return runBench()
	case "security":
		return runSecurity()
	case "mock":
		return runMock()
	case "openapi":
		return runOpenAPI()
	case "migrate":
		return runMigrate()
	case "deadcode":
		return runDeadCode()
	case "update":
		return runInit()
	case "version", "-v", "--version":
		fmt.Printf("go-boost v%s\n", Version)
		return exitOK
	case "help", "-h", "--help":
		printHelp()
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printHelp()
		return exitError
	}
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return exitError
}

func workingDir() (string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fail("failed to get current working directory: %v", err)
	}
	return cwd, exitOK
}

func printHelp() {
	fmt.Print(Banner)
	fmt.Print(`
USAGE:
  go-boost <command> [flags]

COMMANDS:
  run [cmd...]     Supervise command with live error interception & instant diagnostics
  init, install    Initialize go-boost, generate AI guidelines & MCP config
  mcp              Run Model Context Protocol (MCP) server over stdio for AI agents
  status           Analyze current Go project and print stack diagnostics
  routes           Scan and list all detected HTTP routes from AST
  schema           Inspect database schema from discovered connections
  inspect <symbol> Find and inspect a Go struct or interface statically
  check            Run static concurrency hazard audit and go vet
  bench [pkg]      Run benchmarks with memory allocation profiling (-benchmem)
  security         Audit codebase for SQL injection, hardcoded secrets & TLS risks
  mock <interface> Generate a pure-Go unit test mock struct for an interface
  openapi          Generate OpenAPI 3.0 specification JSON from AST routes
  migrate          Generate SQL up/down migrations from schema-struct diffs
  deadcode         Detect unused structs, functions, or interfaces statically
  update           Re-scan project and synchronize AI guidelines & skills
  version          Display go-boost version
  help             Show this help menu

EXAMPLES:
  # Run application with instant error diagnostics
  $ go-boost run make run
  $ go-boost run go run ./...

  # Initialize AI guidelines and MCP configs in your project
  $ go-boost init

  # Start MCP server (used by Cursor, Claude Code, Antigravity, etc.)
  $ go-boost mcp

  # Run security audit on your codebase
  $ go-boost security

  # Generate mock struct for an interface
  $ go-boost mock UserRepository

  # Generate OpenAPI 3.0 JSON specification
  $ go-boost openapi
`)
}

func runMCPServer() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	srv := mcp.NewServer("go-boost", Version,
		mcp.WithInstructions("go-boost provides native MCP tools, prompts, and dynamic resources to accelerate Go development, static AST inspection, database schema exploration, and runtime diagnostics."),
		mcp.WithCompletionSource(mcp.NewCompletionSource(cwd)),
	)

	mcp.RegisterAllTools(srv, cwd)
	mcp.RegisterAllPrompts(srv)
	mcp.RegisterAllResources(srv, cwd)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Serve(ctx); err != nil {
		srv.Log("Server terminated: %v", err)
	}
	return exitOK
}

func runInit() int {
	fmt.Print(Banner)
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	d := detector.NewDetector(cwd)
	stack, err := d.Detect()
	if err != nil {
		return fail("Error detecting stack: %v", err)
	}

	fmt.Printf("🔍 Detected Go Module : %s\n", stack.ModuleName)
	fmt.Printf("📦 Go Version         : %s\n", stack.GoVersion)
	fmt.Printf("🚀 Web Framework      : %s (%s)\n", stack.Framework, stack.FrameworkVersion)
	fmt.Printf("🗄️  ORM / DB Layer     : %s\n", stack.ORM)
	fmt.Printf("💾 Database Engine    : %s\n", stack.DatabaseEngine)
	fmt.Printf("📝 Logger             : %s\n", stack.Logger)
	fmt.Printf("🏛️  Architecture       : %s\n", stack.Architecture)
	fmt.Println()

	binaryName, err := os.Executable()
	if err != nil {
		binaryName = "go-boost"
	}

	res, err := generator.GenerateProjectArtifacts(cwd, binaryName)
	if err != nil {
		return fail("Error generating artifacts: %v", err)
	}

	if len(res.GeneratedFiles) > 0 {
		fmt.Println("✨ Created:")
		for _, f := range res.GeneratedFiles {
			fmt.Printf("   ✅ %s\n", f)
		}
	}
	if len(res.UpdatedFiles) > 0 {
		fmt.Println("♻️  Updated (your own content was kept):")
		for _, f := range res.UpdatedFiles {
			fmt.Printf("   ✅ %s\n", f)
		}
	}
	if len(res.SkippedFiles) > 0 {
		fmt.Println("⏭️  Left untouched:")
		for _, f := range res.SkippedFiles {
			fmt.Printf("   •  %s\n", f)
		}
	}
	if len(res.InstalledSkill) > 0 {
		fmt.Printf("🧠 Skills installed into .claude/skills: %s\n", strings.Join(res.InstalledSkill, ", "))
	}

	fmt.Println("\n🤖 Next Steps for AI Coding Assistants:")
	fmt.Println("  - Cursor         : Automatic! .cursor/mcp.json and .cursorrules are configured.")
	fmt.Println("  - Claude Code    : Automatic! CLAUDE.md and .mcp.json are configured.")
	fmt.Println("  - Antigravity    : In chat, run MCP tool 'app_info' to connect.")
	fmt.Println("  - VS Code Copilot: Reload window; .vscode/mcp.json is configured.")
	fmt.Println("\n🚀 Run 'go-boost status' anytime to inspect project health.")
	return exitOK
}

func runStatus() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("status", flag.ExitOnError)
	formatFlag := fs.String("format", "text", "Output format: text, json")
	_ = fs.Parse(os.Args[2:])

	d := detector.NewDetector(cwd)
	stack, err := d.Detect()
	if err != nil {
		return fail("Error detecting stack: %v", err)
	}

	conns, _ := database.DiscoverConnections(cwd)
	routes, _ := astparser.ScanRoutes(cwd)

	if *formatFlag == "json" {
		out := map[string]any{
			"stack":       stack,
			"connections": conns,
			"routes":      routes,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return exitOK
	}

	fmt.Println("================================================================")
	fmt.Printf("  go-boost Project Health & Stack Analysis\n")
	fmt.Println("================================================================")
	fmt.Printf("  Module Name     : %s\n", stack.ModuleName)
	fmt.Printf("  Go Version      : %s\n", stack.GoVersion)
	fmt.Printf("  Web Framework   : %s\n", stack.Framework)
	fmt.Printf("  ORM Layer       : %s\n", stack.ORM)
	fmt.Printf("  Database Engine : %s\n", stack.DatabaseEngine)
	fmt.Printf("  Logger          : %s\n", stack.Logger)
	fmt.Printf("  Architecture    : %s\n", stack.Architecture)
	fmt.Printf("  Total Packages  : %d dependencies\n", len(stack.Packages))
	fmt.Printf("  HTTP Routes     : %d endpoints detected\n", len(routes))
	if conns != nil {
		fmt.Printf("  DB Connections  : %d connection(s) found\n", len(conns.Connections))
	}
	fmt.Println("================================================================")
	return exitOK
}

func runRoutes() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("routes", flag.ExitOnError)
	formatFlag := fs.String("format", "text", "Output format: text, json")
	_ = fs.Parse(os.Args[2:])

	routes, err := astparser.ScanRoutes(cwd)
	if err != nil {
		return fail("Error scanning routes: %v", err)
	}

	if *formatFlag == "json" {
		data, _ := json.MarshalIndent(routes, "", "  ")
		fmt.Println(string(data))
		return exitOK
	}

	if len(routes) == 0 {
		fmt.Println("No HTTP routes detected in current directory.")
		return exitOK
	}

	fmt.Printf("Found %d HTTP route(s):\n\n", len(routes))
	fmt.Printf("%-8s %-30s %-30s %s\n", "METHOD", "PATH", "HANDLER", "LOCATION")
	fmt.Println("-----------------------------------------------------------------------------------------")
	for _, r := range routes {
		loc := fmt.Sprintf("%s:%d", r.File, r.Line)
		fmt.Printf("%-8s %-30s %-30s %s\n", r.Method, r.Path, r.Handler, loc)
	}
	return exitOK
}

func runSchema() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("schema", flag.ExitOnError)
	summaryFlag := fs.Bool("summary", true, "Show summary only")
	filterFlag := fs.String("filter", "", "Filter tables by name")
	_ = fs.Parse(os.Args[2:])

	conns, err := database.DiscoverConnections(cwd)
	if err != nil || len(conns.Connections) == 0 {
		fmt.Println("No database configuration detected. Add a .env or SQLite file.")
		return exitOK
	}

	res, err := database.Introspect(context.Background(), &conns.Connections[0], database.SchemaOptions{
		Summary: *summaryFlag,
		Filter:  *filterFlag,
	})
	if err != nil {
		return fail("Schema error: %v", err)
	}

	data, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(data))
	return exitOK
}

func runInspect() int {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go-boost inspect <symbol_name>")
		return exitOK
	}
	symbolName := os.Args[2]

	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	symbols, err := astparser.ParsePath(cwd)
	if err != nil {
		return fail("Error parsing AST: %v", err)
	}

	found := false
	for _, s := range symbols.Structs {
		if strings.EqualFold(s.Name, symbolName) {
			found = true
			fmt.Printf("📦 Struct: %s\n", s.Name)
			if s.Doc != "" {
				fmt.Printf("   Doc: %s\n", s.Doc)
			}
			fmt.Printf("   Location: %s:%d\n", s.File, s.Line)
			fmt.Println("   Fields:")
			for _, f := range s.Fields {
				tagStr := ""
				if f.Tag != "" {
					tagStr = fmt.Sprintf(" `%s`", f.Tag)
				}
				fmt.Printf("     - %s: %s%s\n", f.Name, f.Type, tagStr)
			}
			fmt.Println()
		}
	}

	for _, iface := range symbols.Interfaces {
		if strings.EqualFold(iface.Name, symbolName) {
			found = true
			fmt.Printf("🔌 Interface: %s\n", iface.Name)
			if iface.Doc != "" {
				fmt.Printf("   Doc: %s\n", iface.Doc)
			}
			fmt.Printf("   Location: %s:%d\n", iface.File, iface.Line)
			fmt.Println("   Methods:")
			for _, m := range iface.Methods {
				fmt.Printf("     - %s(%s) (%s)\n", m.Name, strings.Join(m.Params, ", "), strings.Join(m.Returns, ", "))
			}
			fmt.Println()
		}
	}

	if !found {
		fmt.Printf("Symbol '%s' not found in AST.\n", symbolName)
	}
	return exitOK
}

func runCheck() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("check", flag.ExitOnError)
	formatFlag := fs.String("format", "text", "Output format: text, json")
	_ = fs.Parse(os.Args[2:])

	issues, err := astparser.CheckConcurrency(cwd)
	diag, _ := runner.CodeCheck(context.Background(), cwd, "./...")

	vetFailed := strings.HasPrefix(diag, "Diagnostics")

	if *formatFlag == "json" {
		out := map[string]any{
			"concurrency_issues": issues,
			"code_check":         diag,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return findingsExit(len(issues) > 0 || vetFailed)
	}

	fmt.Println("🔍 Running static concurrency hazard analysis...")
	if err != nil {
		return fail("Concurrency check error: %v", err)
	}
	if len(issues) == 0 {
		fmt.Println("✅ Concurrency audit passed: No goroutine leaks or mutex issues detected.")
	} else {
		fmt.Printf("⚠️ Found %d concurrency issue(s):\n", len(issues))
		for _, iss := range issues {
			fmt.Printf("   [%s] %s:%d - %s\n", iss.Severity, iss.File, iss.Line, iss.Message)
			fmt.Printf("        Fix: %s\n", iss.Remediation)
		}
	}

	fmt.Println("\n🔍 Running `go vet` and compilation checks...")
	fmt.Println(diag)
	return findingsExit(len(issues) > 0 || vetFailed)
}

func findingsExit(hasFindings bool) int {
	if hasFindings {
		return exitFindings
	}
	return exitOK
}

func runBench() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	pkg := "./..."
	if len(os.Args) >= 3 {
		pkg = os.Args[2]
	}

	fmt.Printf("⏱️ Running benchmarks on package '%s' with memory profiling (-benchmem)...\n", pkg)
	report, err := runner.RunBenchmark(context.Background(), cwd, pkg, ".")
	if err != nil {
		return fail("Benchmark error: %v", err)
	}

	if len(report.Items) == 0 {
		fmt.Println("No benchmark functions found. (Create functions starting with 'Benchmark' in *_test.go files)")
		return exitOK
	}

	fmt.Printf("\n%-35s %-12s %-15s %-12s %-12s\n", "BENCHMARK", "ITERATIONS", "SPEED", "MEMORY", "ALLOCS")
	fmt.Println("---------------------------------------------------------------------------------------------")
	for _, it := range report.Items {
		speed := fmt.Sprintf("%.2f ns/op", it.NsPerOp)
		mem := fmt.Sprintf("%d B/op", it.BytesPerOp)
		allocs := fmt.Sprintf("%d allocs/op", it.AllocsPerOp)
		fmt.Printf("%-35s %-12d %-15s %-12s %-12s\n", it.Name, it.Iterations, speed, mem, allocs)
	}
	fmt.Printf("\n%s\n", report.Summary)
	return exitOK
}

func runSecurity() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("security", flag.ExitOnError)
	formatFlag := fs.String("format", "text", "Output format: text, json, sarif")
	_ = fs.Parse(os.Args[2:])

	vulns, err := security.ScanCodebase(cwd)
	if err != nil {
		return fail("Security scan error: %v", err)
	}

	if *formatFlag == "json" {
		data, _ := json.MarshalIndent(vulns, "", "  ")
		fmt.Println(string(data))
		return findingsExit(len(vulns) > 0)
	}

	if *formatFlag == "sarif" {
		fmt.Println(formatSARIF(vulns))
		return findingsExit(len(vulns) > 0)
	}

	fmt.Println("🛡️  Running static security and vulnerability audit...")
	if len(vulns) == 0 {
		fmt.Println("✅ Security audit passed: No SQL injection, hardcoded secrets, or insecure TLS detected.")
		return exitOK
	}

	fmt.Printf("🚨 Found %d potential security vulnerability(ies):\n\n", len(vulns))
	for _, v := range vulns {
		fmt.Printf("   [%s] %s (File: %s:%d)\n", v.Severity, v.Message, v.File, v.Line)
		fmt.Printf("        Remediation: %s\n\n", v.Remediation)
	}
	return exitFindings
}

func runMock() int {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go-boost mock <InterfaceName>")
		return exitOK
	}
	ifaceName := os.Args[2]

	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	mockCode, err := generator.GenerateMockCode(cwd, ifaceName)
	if err != nil {
		return fail("Error: %v", err)
	}

	fmt.Println(mockCode)
	return exitOK
}

func runOpenAPI() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	d := detector.NewDetector(cwd)
	stack, _ := d.Detect()
	title := "API Specification"
	if stack != nil && stack.ModuleName != "" {
		title = stack.ModuleName + " API"
	}

	spec, err := generator.GenerateOpenAPISpec(cwd, title)
	if err != nil {
		return fail("Error generating OpenAPI spec: %v", err)
	}

	fmt.Println(spec)
	return exitOK
}

func runMigrate() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	conns, err := database.DiscoverConnections(cwd)
	if err != nil || len(conns.Connections) == 0 {
		fmt.Println("No database configuration detected. Add a .env or SQLite file.")
		return exitOK
	}

	fmt.Println("🔄 Comparing database schema with Go struct models...")
	files, err := database.GenerateMigrationScaffold(context.Background(), &conns.Connections[0], cwd)
	if err != nil {
		return fail("Migration generation: %v", err)
	}

	fmt.Println("✨ Successfully generated migration files:")
	fmt.Printf("   Up  : %s\n", files.UpPath)
	fmt.Printf("   Down: %s\n", files.DownPath)
	return exitOK
}

func runDeadCode() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	fs := flag.NewFlagSet("deadcode", flag.ExitOnError)
	formatFlag := fs.String("format", "text", "Output format: text, json")
	_ = fs.Parse(os.Args[2:])

	unused, err := astparser.DetectDeadCode(cwd)
	if err != nil {
		return fail("Dead code check error: %v", err)
	}

	if *formatFlag == "json" {
		data, _ := json.MarshalIndent(unused, "", "  ")
		fmt.Println(string(data))
		return findingsExit(len(unused) > 0)
	}

	fmt.Println("🔍 Scanning AST for unreferenced/dead declarations...")
	if len(unused) == 0 {
		fmt.Println("✅ No dead or unreferenced symbols detected.")
		return exitOK
	}

	fmt.Printf("⚠️ Found %d unreferenced declaration(s):\n\n", len(unused))
	fmt.Printf("%-12s %-30s %s\n", "KIND", "NAME", "LOCATION")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, u := range unused {
		loc := fmt.Sprintf("%s:%d", u.File, u.Line)
		fmt.Printf("%-12s %-30s %s\n", u.Kind, u.Name, loc)
	}
	return exitFindings
}

func formatSARIF(vulns []security.SecurityVulnerability) string {
	type SarifLocation struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine int `json:"startLine"`
			} `json:"region"`
		} `json:"physicalLocation"`
	}

	type SarifResult struct {
		RuleID  string `json:"ruleId"`
		Level   string `json:"level"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
		Locations []SarifLocation `json:"locations"`
	}

	type SarifReport struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name           string `json:"name"`
					Version        string `json:"version"`
					InformationURI string `json:"informationUri"`
				} `json:"driver"`
			} `json:"tool"`
			Results []SarifResult `json:"results"`
		} `json:"runs"`
	}

	var results []SarifResult
	for _, v := range vulns {
		ruleID := "GO-BOOST-" + strings.ToUpper(strings.ReplaceAll(v.Type, "_", "-"))
		level := "warning"
		if v.Severity == "CRITICAL" {
			level = "error"
		} else if v.Severity == "LOW" {
			level = "note"
		}

		var loc SarifLocation
		loc.PhysicalLocation.ArtifactLocation.URI = v.File
		loc.PhysicalLocation.Region.StartLine = v.Line

		var res SarifResult
		res.RuleID = ruleID
		res.Level = level
		res.Message.Text = fmt.Sprintf("%s: %s", v.Message, v.Remediation)
		res.Locations = []SarifLocation{loc}
		results = append(results, res)
	}

	var report SarifReport
	report.Schema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"
	report.Version = "2.1.0"
	report.Runs = []struct {
		Tool struct {
			Driver struct {
				Name           string `json:"name"`
				Version        string `json:"version"`
				InformationURI string `json:"informationUri"`
			} `json:"driver"`
		} `json:"tool"`
		Results []SarifResult `json:"results"`
	}{
		{
			Tool: struct {
				Driver struct {
					Name           string `json:"name"`
					Version        string `json:"version"`
					InformationURI string `json:"informationUri"`
				} `json:"driver"`
			}{
				Driver: struct {
					Name           string `json:"name"`
					Version        string `json:"version"`
					InformationURI string `json:"informationUri"`
				}{
					Name:           "go-boost-security",
					Version:        Version,
					InformationURI: "https://github.com/wahyunoerr/go-boost",
				},
			},
			Results: results,
		},
	}

	data, _ := json.MarshalIndent(report, "", "  ")
	return string(data)
}

func runSupervisedApp() int {
	cwd, code := workingDir()
	if code != exitOK {
		return code
	}

	var cmdArgs []string
	if len(os.Args) > 2 {
		cmdArgs = os.Args[2:]
	} else {
		if hasMakefileRunTarget(cwd) {
			cmdArgs = []string{"make", "run"}
		} else {
			entry := detectMainEntry(cwd)
			cmdArgs = []string{"go", "run", entry}
		}
	}

	fmt.Printf("🚀 go-boost supervising: %s\n\n", strings.Join(cmdArgs, " "))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runner.RunSupervised(ctx, cwd, cmdArgs); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return fail("%v", err)
	}
	return exitOK
}

func hasMakefileRunTarget(rootDir string) bool {
	content, err := os.ReadFile(filepath.Join(rootDir, "Makefile"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			continue
		}
		name, _, found := strings.Cut(l, ":")
		if !found {
			continue
		}
		for _, target := range strings.Fields(name) {
			if target == "run" {
				return true
			}
		}
	}
	return false
}

func detectMainEntry(rootDir string) string {
	return mcp.DetectMainEntry(rootDir)
}
