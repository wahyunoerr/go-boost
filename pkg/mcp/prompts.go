package mcp

import (
	"context"
	"fmt"
)

func RegisterAllPrompts(s *Server) {
	s.RegisterPrompt(Prompt{
		Name:        "generate_table_tests",
		Description: "Prompt template to scaffold idiomatic table-driven Go tests for a target function or struct.",
		Arguments: []PromptArgument{
			{Name: "symbol", Description: "Function or struct name to test", Required: true},
			{Name: "file", Description: "Path to source file", Required: false},
		},
	}, func(ctx context.Context, args map[string]string) (*GetPromptResult, error) {
		symbol := args["symbol"]
		file := args["file"]
		instruction := fmt.Sprintf(`Write comprehensive, idiomatic Go table-driven unit tests for symbol '%s' located in '%s'.
Requirements:
1. Define test cases as a slice of anonymous structs with fields 'name', inputs, mock expectations, 'want', and 'wantErr'.
2. Use t.Run(tc.name, func(t *testing.T) { ... }) for sub-tests.
3. Propagate context.Context where appropriate.
4. Verify both happy paths and edge cases (invalid inputs, not found, database/IO errors).`, symbol, file)

		return &GetPromptResult{
			Description: "Scaffold table-driven tests for " + symbol,
			Messages: []PromptMessage{
				{Role: "user", Content: NewTextContent(instruction)},
			},
		}, nil
	})

	s.RegisterPrompt(Prompt{
		Name:        "refactor_clean_arch",
		Description: "Prompt template to refactor a Go feature into Clean Architecture layers.",
		Arguments: []PromptArgument{
			{Name: "feature", Description: "Feature or entity name (e.g. 'Order' or 'Auth')", Required: true},
		},
	}, func(ctx context.Context, args map[string]string) (*GetPromptResult, error) {
		feature := args["feature"]
		instruction := fmt.Sprintf(`Refactor or scaffold the '%s' feature into idiomatic Clean Architecture layers in Go:
1. Domain layer (internal/domain): Define pure business entities, sentinel errors, and repository interface contracts.
2. Repository layer (internal/repository): Implement database queries and map rows to domain entities.
3. Usecase/Service layer (internal/usecase): Coordinate business logic and transaction boundaries.
4. Delivery/Handler layer (internal/handler): Parse request DTOs, invoke usecase, and serialize HTTP responses.`, feature)

		return &GetPromptResult{
			Description: "Clean Architecture refactoring for " + feature,
			Messages: []PromptMessage{
				{Role: "user", Content: NewTextContent(instruction)},
			},
		}, nil
	})

	s.RegisterPrompt(Prompt{
		Name:        "concurrency_audit",
		Description: "Prompt template to audit concurrency safety, goroutine leaks, and mutex hygiene in a package.",
		Arguments: []PromptArgument{
			{Name: "package", Description: "Package path to audit", Required: true},
		},
	}, func(ctx context.Context, args map[string]string) (*GetPromptResult, error) {
		pkg := args["package"]
		instruction := fmt.Sprintf(`Audit package '%s' for Go concurrency safety and goroutine hygiene:
1. Check that every spawned goroutine has a guaranteed exit path via ctx.Done() or channel termination.
2. Verify mutex lock pairs: ensure defer mu.Unlock() is called immediately after mu.Lock().
3. Check context propagation: ensure context.WithCancel / WithTimeout always has defer cancel().
4. Recommend errgroup.Group for concurrent fan-out tasks where appropriate.`, pkg)

		return &GetPromptResult{
			Description: "Concurrency safety audit for " + pkg,
			Messages: []PromptMessage{
				{Role: "user", Content: NewTextContent(instruction)},
			},
		}, nil
	})

	s.RegisterPrompt(Prompt{
		Name:        "diagnose_panic",
		Description: "Prompt template to diagnose and resolve the last backend error or panic captured in logs.",
	}, func(ctx context.Context, args map[string]string) (*GetPromptResult, error) {
		instruction := `Inspect the most recent backend error or goroutine panic using the go-boost 'last_error' tool.
Steps:
1. Call 'last_error' to get the stack trace, error message, and exact source file:line.
2. Locate and explain the root cause (e.g. nil pointer dereference, slice bounds out of range, unhandled error).
3. Provide a resilient fix with proper defensive checks and error wrapping.`

		return &GetPromptResult{
			Description: "Diagnose and fix last backend panic or error",
			Messages: []PromptMessage{
				{Role: "user", Content: NewTextContent(instruction)},
			},
		}, nil
	})
}
