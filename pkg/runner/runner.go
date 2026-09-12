package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
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

func RunTests(ctx context.Context, rootDir string, opts TestOptions) (*TestResult, error) {
	pkg := opts.Package
	if pkg == "" {
		pkg = "./..."
	}

	args := []string{"test", "-v"}
	if opts.Run != "" {
		args = append(args, "-run", opts.Run)
	}
	if opts.Coverage {
		args = append(args, "-cover")
	}
	if opts.Bench {
		args = append(args, "-bench", ".")
	}
	if opts.Timeout != "" {
		args = append(args, "-timeout", opts.Timeout)
	} else {
		args = append(args, "-timeout", "60s")
	}
	args = append(args, pkg)

	start := time.Now()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = rootDir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond).String()

	raw := outBuf.String()
	rawOutput := raw
	const maxOutputBytes = 12 * 1024
	if len(raw) > maxOutputBytes {
		rawOutput = raw[:maxOutputBytes] + fmt.Sprintf("\n... [truncated %d bytes of test output to preserve AI context]", len(raw)-maxOutputBytes)
	}

	res := &TestResult{
		Success:       err == nil,
		RawOutput:     rawOutput,
		ExecutionTime: elapsed,
		FailedTests:   make([]FailedTest, 0),
	}

	covRegex := regexp.MustCompile(`coverage:\s+(\d+\.\d+%)`)
	if match := covRegex.FindStringSubmatch(raw); len(match) > 1 {
		res.Coverage = match[1]
	}

	lines := strings.Split(raw, "\n")
	var currentFail *FailedTest

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)

		if strings.HasPrefix(trimmed, "--- FAIL:") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				testName := parts[2]
				currentFail = &FailedTest{
					TestName: testName,
				}
				res.FailedTests = append(res.FailedTests, *currentFail)
			}
			continue
		}

		if currentFail != nil && strings.Contains(trimmed, ".go:") {
			idx := strings.Index(trimmed, ".go:")
			prefix := trimmed[:idx+3]
			msg := strings.TrimSpace(trimmed[idx+4:])

			parts := strings.Split(prefix, ":")
			if len(parts) >= 1 {
				currentFail.File = parts[0]
			}
			if len(msg) > 0 {
				colonIdx := strings.Index(msg, ":")
				if colonIdx > 0 {
					currentFail.Line = msg[:colonIdx]
					currentFail.Message = strings.TrimSpace(msg[colonIdx+1:])
				} else {
					currentFail.Message = msg
				}
			}
		}

		if strings.HasPrefix(trimmed, "FAIL\t") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 && currentFail != nil {
				currentFail.Package = parts[1]
			}
		}
	}

	if res.Success {
		res.Summary = fmt.Sprintf("All tests passed successfully in %s", elapsed)
		if res.Coverage != "" {
			res.Summary += fmt.Sprintf(" (Coverage: %s)", res.Coverage)
		}
	} else {
		res.Summary = fmt.Sprintf("Test run failed in %s with %d failure(s)", elapsed, len(res.FailedTests))
	}

	return res, nil
}

func LookupDoc(ctx context.Context, rootDir string, symbol string) (string, error) {
	if symbol == "" {
		return "", fmt.Errorf("no symbol or package specified")
	}

	cmd := exec.CommandContext(ctx, "go", "doc", symbol)
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

	cmd := exec.CommandContext(ctx, "go", "vet", targetPkg)
	cmd.Dir = rootDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Diagnostics / Issues found:\n%s", string(out)), nil
	}

	return "No issues found by `go vet`. Code is clean.", nil
}
