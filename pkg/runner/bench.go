package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type BenchmarkReport struct {
	Success   bool            `json:"success"`
	Package   string          `json:"package"`
	Items     []BenchmarkItem `json:"items"`
	Summary   string          `json:"summary"`
	RawOutput string          `json:"raw_output,omitempty"`
}

type BenchmarkItem struct {
	Name        string  `json:"name"`
	Iterations  int64   `json:"iterations"`
	NsPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  int64   `json:"bytes_per_op"`
	AllocsPerOp int64   `json:"allocs_per_op"`
}

func RunBenchmark(ctx context.Context, rootDir string, pkg string, filter string) (*BenchmarkReport, error) {
	if pkg == "" {
		pkg = "./..."
	}
	if filter == "" {
		filter = "."
	}

	args := []string{"test", "-bench=" + filter, "-benchmem", "-run=^$", pkg}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = rootDir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond).String()

	raw := outBuf.String()
	report := &BenchmarkReport{
		Success:   err == nil,
		Package:   pkg,
		Items:     make([]BenchmarkItem, 0),
		RawOutput: raw,
	}

	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Benchmark") {
			item := parseBenchmarkLine(trimmed)
			if item != nil {
				report.Items = append(report.Items, *item)
			}
		}
	}

	report.Summary = fmt.Sprintf("Completed %d benchmark(s) in %s", len(report.Items), elapsed)
	return report, nil
}

func parseBenchmarkLine(line string) *BenchmarkItem {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return nil
	}

	item := &BenchmarkItem{
		Name: parts[0],
	}

	if iters, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
		item.Iterations = iters
	}

	for i := 2; i < len(parts)-1; i++ {
		val := parts[i]
		unit := parts[i+1]

		switch unit {
		case "ns/op":
			if ns, err := strconv.ParseFloat(val, 64); err == nil {
				item.NsPerOp = ns
			}
		case "B/op":
			if b, err := strconv.ParseInt(val, 10, 64); err == nil {
				item.BytesPerOp = b
			}
		case "allocs/op":
			if a, err := strconv.ParseInt(val, 10, 64); err == nil {
				item.AllocsPerOp = a
			}
		}
	}

	return item
}
