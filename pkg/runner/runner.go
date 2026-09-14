package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	maxOutputBytes     = 12 * 1024
	defaultTestTimeout = "60s"
)

type TestOptions struct {
	Package  string `json:"package"`
	Run      string `json:"run,omitempty"`
	Coverage bool   `json:"coverage,omitempty"`
	Bench    bool   `json:"bench,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

type TestResult struct {
	Success       bool         `json:"success"`
	Summary       string       `json:"summary"`
	Coverage      string       `json:"coverage,omitempty"`
	BuildErrors   []string     `json:"build_errors,omitempty"`
	FailedTests   []FailedTest `json:"failed_tests,omitempty"`
	RawOutput     string       `json:"raw_output,omitempty"`
	ExecutionTime string       `json:"execution_time"`
}

type FailedTest struct {
	TestName string `json:"test_name"`
	Package  string `json:"package"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     string `json:"line,omitempty"`
}

type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

var (
	coverageRegex            = regexp.MustCompile(`coverage:\s+(\d+\.\d+%)`)
	testFailureLocationRegex = regexp.MustCompile(`^\s*([\w\-./]+\.go):(\d+):\s*(.*)$`)
)

func validateGoTarget(target string) error {
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("package target %q is not allowed: arguments starting with '-' are interpreted as go tool flags", target)
	}
	return nil
}

func RunTests(ctx context.Context, rootDir string, opts TestOptions) (*TestResult, error) {
	pkg := opts.Package
	if pkg == "" {
		pkg = "./..."
	}
	if err := validateGoTarget(pkg); err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout == "" {
		timeout = defaultTestTimeout
	}

	args := []string{"test", "-json"}
	if opts.Run != "" {
		args = append(args, "-run", opts.Run)
	}
	if opts.Coverage {
		args = append(args, "-cover")
	}
	if opts.Bench {
		args = append(args, "-bench", ".")
	}
	args = append(args, "-timeout", timeout, "--", pkg)

	start := time.Now()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = rootDir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	runErr := cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond).String()

	res := parseTestJSON(outBuf.String())
	res.Success = runErr == nil
	res.ExecutionTime = elapsed

	if res.Success {
		res.Summary = fmt.Sprintf("All tests passed successfully in %s", elapsed)
		if res.Coverage != "" {
			res.Summary += fmt.Sprintf(" (Coverage: %s)", res.Coverage)
		}
		return res, nil
	}

	switch {
	case len(res.BuildErrors) > 0 && len(res.FailedTests) == 0:
		res.Summary = fmt.Sprintf("Build failed in %s with %d error(s); no tests ran", elapsed, len(res.BuildErrors))
	case len(res.FailedTests) > 0:
		res.Summary = fmt.Sprintf("Test run failed in %s with %d failure(s)", elapsed, len(res.FailedTests))
	default:
		res.Summary = fmt.Sprintf("Test run failed in %s; see raw_output for details", elapsed)
	}

	return res, nil
}

func parseTestJSON(raw string) *TestResult {
	res := &TestResult{FailedTests: make([]FailedTest, 0)}

	type testKey struct{ pkg, test string }
	output := make(map[testKey][]string)

	var plainLines []string
	buildOutput := make(map[string][]string)

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				plainLines = append(plainLines, trimmed)
			}
			continue
		}

		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		if match := coverageRegex.FindStringSubmatch(ev.Output); len(match) > 1 && res.Coverage == "" {
			res.Coverage = match[1]
		}

		key := testKey{ev.Package, ev.Test}

		switch ev.Action {
		case "output":
			if ev.Test == "" {
				buildOutput[ev.Package] = append(buildOutput[ev.Package], ev.Output)
			} else {
				output[key] = append(output[key], ev.Output)
			}
		case "build-output":
			buildOutput[ev.Package] = append(buildOutput[ev.Package], ev.Output)
		case "fail":
			if ev.Test == "" {
				continue
			}
			res.FailedTests = append(res.FailedTests, buildFailedTest(ev.Package, ev.Test, output[key]))
		case "build-fail":
			res.BuildErrors = append(res.BuildErrors, collectBuildError(ev.Package, buildOutput[ev.Package]))
		}
	}

	if len(res.BuildErrors) == 0 {
		for pkg, lines := range buildOutput {
			joined := strings.Join(lines, "")
			if strings.Contains(joined, "[build failed]") || strings.Contains(joined, "[setup failed]") {
				res.BuildErrors = append(res.BuildErrors, collectBuildError(pkg, lines))
			}
		}
	}
	if len(plainLines) > 0 {
		res.BuildErrors = append(res.BuildErrors, strings.Join(plainLines, "\n"))
	}

	res.RawOutput = truncateTestOutput(raw, res.FailedTests, res.BuildErrors)
	return res
}

func buildFailedTest(pkg, test string, lines []string) FailedTest {
	ft := FailedTest{TestName: test, Package: pkg}

	var messages []string
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(strings.TrimSpace(trimmed), "=== RUN") ||
			strings.HasPrefix(strings.TrimSpace(trimmed), "=== PAUSE") ||
			strings.HasPrefix(strings.TrimSpace(trimmed), "=== CONT") ||
			strings.HasPrefix(strings.TrimSpace(trimmed), "--- FAIL") ||
			strings.TrimSpace(trimmed) == "" {
			continue
		}

		if match := testFailureLocationRegex.FindStringSubmatch(trimmed); len(match) > 3 {
			if ft.File == "" {
				ft.File = match[1]
				ft.Line = match[2]
			}
			if msg := strings.TrimSpace(match[3]); msg != "" {
				messages = append(messages, msg)
			}
			continue
		}

		messages = append(messages, strings.TrimSpace(trimmed))
	}

	ft.Message = strings.Join(messages, "\n")
	return ft
}

func collectBuildError(pkg string, lines []string) string {
	joined := strings.TrimSpace(strings.Join(lines, ""))
	if pkg == "" {
		return joined
	}
	return pkg + ": " + joined
}

func truncateTestOutput(raw string, failures []FailedTest, buildErrors []string) string {
	readable := rawTestText(raw)
	if len(readable) <= maxOutputBytes {
		return readable
	}

	if len(failures) > 0 || len(buildErrors) > 0 {
		tail := readable[len(readable)-maxOutputBytes:]
		if idx := strings.IndexByte(tail, '\n'); idx >= 0 && idx < len(tail)-1 {
			tail = tail[idx+1:]
		}
		return fmt.Sprintf("... [truncated %d bytes of earlier output]\n%s", len(readable)-len(tail), tail)
	}

	head := readable[:maxOutputBytes]
	return head + fmt.Sprintf("\n... [truncated %d bytes of test output to preserve AI context]", len(readable)-maxOutputBytes)
}

func rawTestText(raw string) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			sb.WriteString(line)
			sb.WriteString("\n")
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		sb.WriteString(ev.Output)
	}

	return sb.String()
}

func LookupDoc(ctx context.Context, rootDir string, symbol string) (string, error) {
	if symbol == "" {
		return "", fmt.Errorf("no symbol or package specified")
	}
	if err := validateGoTarget(symbol); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "go", "doc", "--", symbol)
	cmd.Dir = rootDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go doc error: %v, output: %s", err, string(out))
	}

	return string(out), nil
}

func CodeCheck(ctx context.Context, rootDir string, targetPkg string) (string, error) {
	if targetPkg == "" {
		targetPkg = "./..."
	}
	if err := validateGoTarget(targetPkg); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "go", "vet", "--", targetPkg)
	cmd.Dir = rootDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Diagnostics / Issues found:\n%s", string(out)), nil
	}

	return "No issues found by `go vet`. Code is clean.", nil
}
