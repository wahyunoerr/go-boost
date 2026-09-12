package runner

import (
	"context"
	"testing"
)

func TestLookupDoc(t *testing.T) {
	ctx := context.Background()
	doc, err := LookupDoc(ctx, ".", "fmt.Println")
	if err != nil {
		t.Fatalf("LookupDoc failed: %v", err)
	}

	if doc == "" {
		t.Errorf("expected doc for fmt.Println, got empty")
	}
}

func TestCodeCheck(t *testing.T) {
	ctx := context.Background()
	res, err := CodeCheck(ctx, ".", "./pkg/mcp/...")
	if err != nil {
		t.Fatalf("CodeCheck failed: %v", err)
	}

	if res == "" {
		t.Errorf("expected non-empty check result")
	}
}
