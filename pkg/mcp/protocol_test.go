package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPServerHandshakeAndTools(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}

	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	srv.RegisterTool(Tool{
		Name:        "echo_tool",
		Description: "Echoes input back",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"msg": {Type: "string", Description: "Message to echo"},
			},
			Required: []string{"msg"},
		},
	}, func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
		msg, _ := args["msg"].(string)
		return &CallToolResult{
			Content: []Content{NewTextContent("Echo: " + msg)},
		}, nil
	})

	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}` + "\n"

	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"

	callReq := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo_tool","arguments":{"msg":"hello go-boost"}}}` + "\n"

	in.WriteString(initReq)
	in.WriteString(listReq)
	in.WriteString(callReq)

	err := srv.Serve(context.Background())
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 responses, got %d: %s", len(lines), out.String())
	}

	var initResp Response
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("failed to unmarshal init response: %v", err)
	}
	if initResp.ID != float64(1) {
		t.Errorf("expected id 1, got %v", initResp.ID)
	}

	var listResp Response
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	listJSON, _ := json.Marshal(listResp.Result)
	if !strings.Contains(string(listJSON), "echo_tool") {
		t.Errorf("expected echo_tool in tools/list response, got %s", string(listJSON))
	}

	var callResp Response
	if err := json.Unmarshal([]byte(lines[2]), &callResp); err != nil {
		t.Fatalf("failed to unmarshal call response: %v", err)
	}
	callJSON, _ := json.Marshal(callResp.Result)
	if !strings.Contains(string(callJSON), "Echo: hello go-boost") {
		t.Errorf("expected 'Echo: hello go-boost' in call response, got %s", string(callJSON))
	}
}

func TestMCP2026Features(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}

	srv := NewServer("test-2026-server", "2.0.0", WithIO(in, out))

	init2026 := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-01-01","capabilities":{},"clientInfo":{"name":"modern-agent","version":"2.0.0"}}}` + "\n"

	compReq := `{"jsonrpc":"2.0","id":2,"method":"completion/complete","params":{"ref":{"type":"ref/tool","name":"ast_inspect"},"argument":{"name":"path","value":"internal"}}}` + "\n"

	subReq := `{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{"uri":"project://metadata"}}}` + "\n"

	logReq := `{"jsonrpc":"2.0","id":4,"method":"logging/setLevel","params":{"level":"debug"}}}` + "\n"

	in.WriteString(init2026)
	in.WriteString(compReq)
	in.WriteString(subReq)
	in.WriteString(logReq)

	err := srv.Serve(context.Background())
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 responses, got %d: %s", len(lines), out.String())
	}

	var initResp Response
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("failed to unmarshal init response: %v", err)
	}
	initJSON, _ := json.Marshal(initResp.Result)
	if !strings.Contains(string(initJSON), "2026-01-01") {
		t.Errorf("expected 2026-01-01 in init response, got %s", string(initJSON))
	}

	var compResp Response
	if err := json.Unmarshal([]byte(lines[1]), &compResp); err != nil {
		t.Fatalf("failed to unmarshal completion response: %v", err)
	}
	compJSON, _ := json.Marshal(compResp.Result)
	if !strings.Contains(string(compJSON), "internal/domain") {
		t.Errorf("expected completion values in response, got %s", string(compJSON))
	}

	if err := srv.NotifyToolsListChanged(); err != nil {
		t.Errorf("NotifyToolsListChanged failed: %v", err)
	}
	if err := srv.NotifyResourceUpdated("project://metadata"); err != nil {
		t.Errorf("NotifyResourceUpdated failed: %v", err)
	}
}
