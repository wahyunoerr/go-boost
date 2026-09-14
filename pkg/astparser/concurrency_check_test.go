package astparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkSource(t *testing.T, source string) []ConcurrencyIssue {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	issues, err := CheckConcurrency(dir)
	if err != nil {
		t.Fatalf("CheckConcurrency failed: %v", err)
	}
	return issues
}

func issueTypes(issues []ConcurrencyIssue) string {
	if len(issues) == 0 {
		return "(no issues)"
	}
	var sb strings.Builder
	for _, i := range issues {
		sb.WriteString(i.Type + ": " + i.Message + "\n")
	}
	return sb.String()
}

func containsType(issues []ConcurrencyIssue, kind string) bool {
	for _, i := range issues {
		if i.Type == kind {
			return true
		}
	}
	return false
}

func TestCheckConcurrencyDetectsLockHeldOnEarlyReturn(t *testing.T) {
	issues := checkSource(t, `package c

import "sync"

var mu sync.Mutex

func Bad(x bool) int {
	mu.Lock()
	if x {
		return 1
	}
	mu.Unlock()
	return 0
}
`)

	if !containsType(issues, "mutex_leak") {
		t.Errorf("expected the early-return lock leak to be reported, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyDetectsMissingUnlock(t *testing.T) {
	issues := checkSource(t, `package c

import "sync"

type Cache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *Cache) Set(k, v string) {
	c.mu.Lock()
	c.m[k] = v
}
`)

	if !containsType(issues, "mutex_leak") {
		t.Errorf("expected a never-released lock to be reported, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyAcceptsDeferredUnlock(t *testing.T) {
	issues := checkSource(t, `package c

import "sync"

type Cache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *Cache) Get(k string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k == "" {
		return ""
	}
	return c.m[k]
}
`)

	if containsType(issues, "mutex_leak") {
		t.Errorf("defer covers every path; expected no mutex issue, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyReportsUnsupervisedGoroutines(t *testing.T) {
	issues := checkSource(t, `package c

func Spawn(items []int) {
	for range items {
		go func() {
			select {}
		}()
	}
}
`)

	if !containsType(issues, "unsupervised_goroutine") {
		t.Errorf("expected an unsupervised goroutine to be reported, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyAcceptsWaitGroupSupervisedGoroutines(t *testing.T) {
	issues := checkSource(t, `package c

import "sync"

func Spawn(items []int) {
	var wg sync.WaitGroup
	for range items {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()
}
`)

	if containsType(issues, "unsupervised_goroutine") {
		t.Errorf("goroutines are waited on; expected no issue, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyDetectsContextLeak(t *testing.T) {
	issues := checkSource(t, `package c

import (
	"context"
	"time"
)

func Leaky(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	_ = ctx
	_ = cancel
}

func Discarded(ctx context.Context) {
	ctx, _ = context.WithTimeout(ctx, time.Second)
	_ = ctx
}
`)

	count := 0
	for _, i := range issues {
		if i.Type == "context_leak" {
			count++
		}
	}
	if count == 0 {
		t.Errorf("expected context leaks to be reported, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyAcceptsReturnedAndDeferredCancel(t *testing.T) {
	issues := checkSource(t, `package c

import (
	"context"
	"time"
)

func Deferred(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = ctx
}

func Returned(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	return ctx, cancel
}

func DeferredInClosure(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() { cancel() }()
	_ = ctx
}
`)

	if containsType(issues, "context_leak") {
		t.Errorf("expected no context leak issues, got:\n%s", issueTypes(issues))
	}
}

func TestCheckConcurrencyIsDeterministic(t *testing.T) {
	source := `package c

import "sync"

var mu sync.Mutex

func A() { mu.Lock() }
func B() { mu.Lock() }
func C() { mu.Lock() }
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	first, err := CheckConcurrency(dir)
	if err != nil {
		t.Fatalf("CheckConcurrency failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := CheckConcurrency(dir)
		if err != nil {
			t.Fatalf("CheckConcurrency failed: %v", err)
		}
		if len(next) != len(first) {
			t.Fatalf("issue count changed between runs: %d then %d", len(first), len(next))
		}
		for j := range next {
			if next[j].Line != first[j].Line {
				t.Fatalf("issue order changed between runs at index %d", j)
			}
		}
	}
}
