package diagnostics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	compileErrRegex  = regexp.MustCompile(`(?m)^([a-zA-Z]:)?([a-zA-Z0-9_\-\./\\ ]+\.go):([0-9]+)(?::([0-9]+))?:\s*(.+)$`)
	panicHeaderRegex = regexp.MustCompile(`(?m)^panic:\s*(.+)$`)
	goFileLocRegex   = regexp.MustCompile(`((?:[a-zA-Z]:)?[a-zA-Z0-9_\-\./\\]+\.go):([0-9]+)`)
	portPatternRegex = regexp.MustCompile(`(?i)(?:listen\s+tcp\s+[^:]*:|:)([0-9]{2,5})(?::\s*bind|\s*bind)`)
)

type DiagnosticReport struct {
	Category     string `json:"category"`
	Title        string `json:"title"`
	Message      string `json:"message"`
	File         string `json:"file,omitempty"`
	Line         int    `json:"line,omitempty"`
	Column       int    `json:"column,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
	RootCause    string `json:"root_cause,omitempty"`
	SuggestedFix string `json:"suggested_fix,omitempty"`
	Timestamp    string `json:"timestamp"`
	RawOutput    string `json:"raw_output,omitempty"`
}

func AnalyzeErrorOutput(rawOutput string, projectRoot string) *DiagnosticReport {
	trimmed := strings.TrimSpace(rawOutput)
	if trimmed == "" {
		return nil
	}

	now := time.Now().Format(time.RFC3339)

	if strings.Contains(rawOutput, "fatal error: all goroutines are asleep - deadlock!") {
		report := &DiagnosticReport{
			Category:     "Concurrency Deadlock",
			Title:        "Fatal Deadlock Detected",
			Message:      "all goroutines are asleep - deadlock!",
			RootCause:    "All active goroutines are synchronously blocked waiting on channels, sync.Mutex, or sync.WaitGroup with no producer left to wake them.",
			SuggestedFix: "Audit channel communication to ensure every receiver has an active sender, or ensure sync.WaitGroup.Done() / mu.Unlock() is reliably called using defer.",
			Timestamp:    now,
			RawOutput:    rawOutput,
		}
		findProjectLocation(report, rawOutput, projectRoot)
		return report
	}

	if panicMatches := panicHeaderRegex.FindStringSubmatch(rawOutput); len(panicMatches) > 1 {
		panicMsg := strings.TrimSpace(panicMatches[1])
		report := &DiagnosticReport{
			Category:  "Runtime Panic",
			Title:     "Application Crashed with Panic",
			Message:   panicMsg,
			Timestamp: now,
			RawOutput: rawOutput,
		}

		classifyPanic(report, panicMsg)
		findProjectLocation(report, rawOutput, projectRoot)
		return report
	}

	if compileMatches := compileErrRegex.FindStringSubmatch(rawOutput); len(compileMatches) > 1 {
		filePath := compileMatches[1] + compileMatches[2]
		lineNum, _ := strconv.Atoi(compileMatches[3])
		colNum := 0
		if len(compileMatches) > 4 && compileMatches[4] != "" {
			colNum, _ = strconv.Atoi(compileMatches[4])
		}
		errMsg := compileMatches[5]

		report := &DiagnosticReport{
			Category:  "Compilation Error",
			Title:     "Build / Syntax Check Failed",
			Message:   errMsg,
			File:      filePath,
			Line:      lineNum,
			Column:    colNum,
			Timestamp: now,
			RawOutput: rawOutput,
		}

		classifyCompileError(report, errMsg)
		if fullPath := resolvePath(projectRoot, filePath); fullPath != "" {
			report.Snippet = ExtractCodeSnippet(fullPath, lineNum, 2)
		}
		return report
	}

	if strings.Contains(rawOutput, "bind: address already in use") {
		port := "port"
		if m := portPatternRegex.FindStringSubmatch(rawOutput); len(m) > 1 {
			port = m[1]
		}
		return &DiagnosticReport{
			Category:     "Network / Port Conflict",
			Title:        fmt.Sprintf("Port %s Already in Use", port),
			Message:      "bind: address already in use",
			RootCause:    fmt.Sprintf("Another process is currently bound to port %s.", port),
			SuggestedFix: fmt.Sprintf("Terminate the conflicting process using `lsof -ti :%s | xargs kill -9` or change the server port in configuration.", port),
			Timestamp:    now,
			RawOutput:    rawOutput,
		}
	}

	if strings.Contains(rawOutput, "connection refused") {
		return &DiagnosticReport{
			Category:     "Database / Service Connection Failure",
			Title:        "TCP Connection Refused",
			Message:      "connect: connection refused",
			RootCause:    "The target database or external service is not running or is not accepting connections at the specified host:port.",
			SuggestedFix: "Verify that the database service is running (e.g. docker ps, systemctl status) and inspect your host/port credentials in .env.",
			Timestamp:    now,
			RawOutput:    rawOutput,
		}
	}

	if strings.Contains(rawOutput, "password authentication failed") || strings.Contains(rawOutput, "Access denied for user") {
		return &DiagnosticReport{
			Category:     "Database Authentication Error",
			Title:        "Database Credentials Rejected",
			Message:      "authentication failed for user",
			RootCause:    "The database server rejected the provided username or password.",
			SuggestedFix: "Verify the database user, password, and database name in your environment variables or config file.",
			Timestamp:    now,
			RawOutput:    rawOutput,
		}
	}

	for _, line := range strings.Split(rawOutput, "\n") {
		lineTrimmed := strings.TrimSpace(line)
		if strings.Contains(lineTrimmed, `"level":"error"`) || strings.Contains(lineTrimmed, `level=error`) || strings.Contains(lineTrimmed, `[ERROR]`) {
			report := &DiagnosticReport{
				Category:     "Application Log Error",
				Title:        "Critical Error Emitted in Application Log",
				Message:      lineTrimmed,
				RootCause:    "Application runtime recorded an error-level log event.",
				SuggestedFix: "Inspect the message details and caller stack trace to handle this error condition gracefully.",
				Timestamp:    now,
				RawOutput:    rawOutput,
			}
			findProjectLocation(report, rawOutput, projectRoot)
			return report
		}
	}

	return nil
}

func classifyPanic(report *DiagnosticReport, panicMsg string) {
	lower := strings.ToLower(panicMsg)
	switch {
	case strings.Contains(lower, "nil pointer dereference") || strings.Contains(lower, "invalid memory address"):
		report.Title = "Runtime Panic: Nil Pointer Dereference"
		report.RootCause = "Attempted to read or write a struct field or invoke a method on a pointer that is nil."
		report.SuggestedFix = "Check if the pointer variable is nil before accessing its fields or methods (e.g. `if ptr == nil { return ... }`)."
	case strings.Contains(lower, "index out of range"):
		report.Title = "Runtime Panic: Slice Index Out of Range"
		report.RootCause = "Attempted to access a slice or array element with an index greater than or equal to its length."
		report.SuggestedFix = "Verify slice boundary before indexing (e.g. `if idx < len(slice) { ... }`)."
	case strings.Contains(lower, "assignment to entry in nil map"):
		report.Title = "Runtime Panic: Write to Nil Map"
		report.RootCause = "Attempted to store a key-value pair in a map that was declared but never initialized."
		report.SuggestedFix = "Initialize the map using `make(map[KeyType]ValueType)` before assigning values to it."
	case strings.Contains(lower, "close of closed channel"):
		report.Title = "Runtime Panic: Closed Channel Re-closed"
		report.RootCause = "A channel was closed more than once."
		report.SuggestedFix = "Ensure channel ownership hygiene: only the single sender/producer should close the channel, or use sync.Once."
	case strings.Contains(lower, "send on closed channel"):
		report.Title = "Runtime Panic: Send on Closed Channel"
		report.RootCause = "Attempted to send data into a channel that has already been closed."
		report.SuggestedFix = "Synchronize channel closure with send operations using context.Done() or sync.WaitGroup."
	default:
		report.RootCause = "An unrecovered panic occurred during execution."
		report.SuggestedFix = "Inspect the stack trace location to add proper error checking or recover() mechanism."
	}
}

func classifyCompileError(report *DiagnosticReport, errMsg string) {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "undefined:"):
		report.RootCause = "The referenced identifier is not defined in the current package or has not been imported."
		report.SuggestedFix = "Check for typos in symbol name, verify the package export (capitalization), or add the required import."
	case strings.Contains(lower, "cannot use") && strings.Contains(lower, "as type"):
		report.RootCause = "Type mismatch between variable definition and assigned expression or return statement."
		report.SuggestedFix = "Perform explicit type conversion or update the function signature / struct field to matching type."
	case strings.Contains(lower, "imported and not used"):
		report.RootCause = "Go compiler forbids unused package imports."
		report.SuggestedFix = "Remove the unused import, or use `_ \"pkg\"` if imported strictly for initialization side effects."
	case strings.Contains(lower, "syntax error"):
		report.RootCause = "Go parser encountered invalid syntax (e.g. unclosed parenthesis, missing comma, or misplaced token)."
		report.SuggestedFix = "Review the line indicated by the compiler and check for missing delimiters or syntax errors."
	default:
		report.RootCause = "Go compiler was unable to compile the source code."
		report.SuggestedFix = "Fix the compilation error reported above at the specified file and line."
	}
}

func findProjectLocation(report *DiagnosticReport, rawOutput string, projectRoot string) {
	lines := strings.Split(rawOutput, "\n")
	for _, l := range lines {
		lTrimmed := strings.TrimSpace(l)
		if strings.HasPrefix(lTrimmed, "created by ") || strings.HasPrefix(lTrimmed, "goroutine ") {
			continue
		}

		if matches := goFileLocRegex.FindStringSubmatch(lTrimmed); len(matches) > 2 {
			filePath := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])

			if strings.Contains(filePath, "/src/runtime/") || strings.HasPrefix(filePath, "runtime/") {
				continue
			}

			report.File = filePath
			report.Line = lineNum

			if fullPath := resolvePath(projectRoot, filePath); fullPath != "" {
				report.Snippet = ExtractCodeSnippet(fullPath, lineNum, 2)
			}
			return
		}
	}
}

func resolvePath(projectRoot string, filePath string) string {
	if filepath.IsAbs(filePath) {
		if _, err := os.Stat(filePath); err == nil {
			return filePath
		}
	}
	if projectRoot != "" {
		joined := filepath.Join(projectRoot, filePath)
		if _, err := os.Stat(joined); err == nil {
			return joined
		}
	}
	return filePath
}

func ExtractCodeSnippet(filePath string, targetLine int, contextLines int) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	currentLine := 0
	startLine := targetLine - contextLines
	if startLine < 1 {
		startLine = 1
	}
	endLine := targetLine + contextLines

	for scanner.Scan() {
		currentLine++
		if currentLine >= startLine && currentLine <= endLine {
			prefix := "   "
			if currentLine == targetLine {
				prefix = "-> "
			}
			lines = append(lines, fmt.Sprintf("%s%4d | %s", prefix, currentLine, scanner.Text()))
		}
		if currentLine > endLine {
			break
		}
	}

	return strings.Join(lines, "\n")
}

func RenderDiagnosticBox(report *DiagnosticReport) string {
	if report == nil {
		return ""
	}

	var sb strings.Builder
	separator := strings.Repeat("=", 72)
	divider := strings.Repeat("-", 72)

	sb.WriteString("\n" + separator + "\n")
	sb.WriteString(fmt.Sprintf("🚨 GO-BOOST INSTANT DIAGNOSTIC: %s\n", report.Category))
	sb.WriteString(separator + "\n")

	if report.File != "" && report.Line > 0 {
		if report.Column > 0 {
			sb.WriteString(fmt.Sprintf("📍 Location : %s:%d:%d\n", report.File, report.Line, report.Column))
		} else {
			sb.WriteString(fmt.Sprintf("📍 Location : %s:%d\n", report.File, report.Line))
		}
	}

	sb.WriteString(fmt.Sprintf("💥 Message  : %s\n", report.Message))

	if report.Snippet != "" {
		sb.WriteString("\n📄 Source Context:\n")
		sb.WriteString(divider + "\n")
		sb.WriteString(report.Snippet + "\n")
		sb.WriteString(divider + "\n")
	}

	if report.RootCause != "" {
		sb.WriteString(fmt.Sprintf("\n💡 Root Cause:\n   %s\n", report.RootCause))
	}

	if report.SuggestedFix != "" {
		sb.WriteString(fmt.Sprintf("\n🛠️ Suggested Fix:\n   %s\n", report.SuggestedFix))
	}

	sb.WriteString("\n📡 Saved to .go-boost/last_error.json (Synchronized with MCP)\n")
	sb.WriteString(separator + "\n")

	return sb.String()
}

func SaveReport(report *DiagnosticReport, projectRoot string) error {
	if report == nil {
		return nil
	}

	targetDir := projectRoot
	if targetDir == "" {
		targetDir = "."
	}
	boostDir := filepath.Join(targetDir, ".go-boost")
	if err := os.MkdirAll(boostDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	targetFile := filepath.Join(boostDir, "last_error.json")
	return os.WriteFile(targetFile, data, 0644)
}
