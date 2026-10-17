package runner

import (
	"testing"
)

func TestParseBenchmarkLine(t *testing.T) {
	line := "BenchmarkMD5-8   \t 5000000\t       285.50 ns/op\t      48 B/op\t       1 allocs/op"
	item := parseBenchmarkLine(line)

	if item == nil {
		t.Fatalf("expected parsed benchmark item, got nil")
	}

	if item.Name != "BenchmarkMD5-8" {
		t.Errorf("expected name 'BenchmarkMD5-8', got '%s'", item.Name)
	}

	if item.Iterations != 5000000 {
		t.Errorf("expected iterations 5000000, got %d", item.Iterations)
	}

	if item.NsPerOp != 285.50 {
		t.Errorf("expected 285.50 ns/op, got %f", item.NsPerOp)
	}

	if item.BytesPerOp != 48 {
		t.Errorf("expected 48 B/op, got %d", item.BytesPerOp)
	}

	if item.AllocsPerOp != 1 {
		t.Errorf("expected 1 allocs/op, got %d", item.AllocsPerOp)
	}
}
