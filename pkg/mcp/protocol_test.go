package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func drive(t *testing.T, srv *Server, in *bytes.Buffer, out *bytes.Buffer, requests ...string) []Response {
	t.Helper()

	for _, r := range requests {
		in.WriteString(r + "\n")
	}

	if err := srv.Serve(context.Background()); err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	var responses []Response
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("failed to unmarshal %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func resultJSON(t *testing.T, resp Response) string {
	t.Helper()
	data, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	return string(data)
}

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
		return &CallToolResult{Content: []Content{NewTextContent("Echo: " + msg)}}, nil
	})

	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo_tool","arguments":{"msg":"hello go-boost"}}}`,
	)

	if len(responses) < 3 {
		t.Fatalf("expected at least 3 responses, got %d: %s", len(responses), out.String())
	}
	if responses[0].ID != float64(1) {
		t.Errorf("expected id 1, got %v", responses[0].ID)
	}
	if !strings.Contains(resultJSON(t, responses[1]), "echo_tool") {
		t.Errorf("expected echo_tool in tools/list, got %s", resultJSON(t, responses[1]))
	}
	if !strings.Contains(resultJSON(t, responses[2]), "Echo: hello go-boost") {
		t.Errorf("unexpected tools/call result: %s", resultJSON(t, responses[2]))
	}
}

func TestNegotiateProtocolVersion(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{"empty defaults to latest", "", LatestMCPVersion},
		{"2024-11-05 is supported", Version20241105, Version20241105},
		{"2025-03-26 is supported", Version20250326, Version20250326},
		{"2025-06-18 is supported", Version20250618, Version20250618},
		{"2025-11-25 is supported", Version20251125, Version20251125},
		{"unknown falls back to latest", "1999-01-01", LatestMCPVersion},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NegotiateProtocolVersion(tc.requested); got != tc.want {
				t.Errorf("NegotiateProtocolVersion(%q) = %q, want %q", tc.requested, got, tc.want)
			}
		})
	}
}

func TestInitializeEchoesClientProtocolVersion(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"agent","version":"1.0.0"}}}`,
	)

	got := resultJSON(t, responses[0])
	if !strings.Contains(got, Version20250618) {
		t.Errorf("expected the server to accept the client's supported version, got %s", got)
	}
}

func TestCapabilitiesMatchImplementedFeatures(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"agent","version":"1.0.0"}}}`,
	)

	got := resultJSON(t, responses[0])
	for _, unsupported := range []string{"listChanged", "subscribe"} {
		if strings.Contains(got, unsupported) {
			t.Errorf("capabilities advertise %q, which the server never sends: %s", unsupported, got)
		}
	}
}

func TestToolListOrderIsStable(t *testing.T) {
	var previous string

	for i := 0; i < 5; i++ {
		in := &bytes.Buffer{}
		out := &bytes.Buffer{}
		srv := NewServer("test-server", "1.0.0", WithIO(in, out))

		for _, name := range []string{"zeta", "alpha", "mike", "bravo", "yankee"} {
			srv.RegisterTool(Tool{Name: name, InputSchema: ToolSchema{Type: "object"}},
				func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
					return &CallToolResult{}, nil
				})
		}

		responses := drive(t, srv, in, out, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		current := resultJSON(t, responses[0])
		if previous != "" && current != previous {
			t.Fatalf("tools/list order changed between runs:\n%s\n%s", previous, current)
		}
		previous = current
	}

	if !strings.Contains(previous, `"name":"alpha"`) {
		t.Fatalf("unexpected tools/list payload: %s", previous)
	}
	if strings.Index(previous, "alpha") > strings.Index(previous, "bravo") {
		t.Errorf("expected tools to be sorted by name: %s", previous)
	}
}

func TestUnknownLogLevelIsRejected(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"logging/setLevel","params":{"level":"loud"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"logging/setLevel","params":{"level":"debug"}}`,
	)

	if responses[0].Error == nil {
		t.Error("expected an error for an unsupported log level")
	}
	if responses[1].Error != nil {
		t.Errorf("expected 'debug' to be accepted, got %+v", responses[1].Error)
	}
}

func TestMalformedMessageDoesNotStopTheServer(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`this is not json`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)

	if len(responses) != 3 {
		t.Fatalf("expected the server to keep serving after a bad message, got %d responses", len(responses))
	}
	if responses[1].Error == nil || responses[1].Error.Code != ErrCodeParseError {
		t.Errorf("expected a parse error for invalid JSON, got %+v", responses[1].Error)
	}
	if responses[2].Error != nil {
		t.Errorf("expected the request after the bad message to succeed, got %+v", responses[2].Error)
	}
}

func TestOversizedLineDoesNotStopTheServer(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	huge := strings.Repeat("a", 300000)
	responses := drive(t, srv, in, out,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+huge+`"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)

	if len(responses) != 2 {
		t.Fatalf("expected both requests to be answered, got %d", len(responses))
	}
	if responses[1].ID != float64(2) {
		t.Errorf("expected the second ping to be answered, got %+v", responses[1])
	}
}

func TestToolCallCanBeCancelled(t *testing.T) {
	in := &bytes.Buffer{}
	out := &lockedBuffer{}

	srv := NewServer("test-server", "1.0.0", WithIO(in, out))

	started := make(chan struct{})
	srv.RegisterTool(Tool{Name: "slow", InputSchema: ToolSchema{Type: "object"}},
		func(ctx context.Context, args map[string]any) (*CallToolResult, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})

	pr, pw := newPipe()
	srv.in = pr

	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background()) }()

	pw.write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n")
	pw.write(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	pw.write(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"slow","arguments":{}}}` + "\n")

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool never started")
	}

	pw.write(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"user stopped"}}` + "\n")
	pw.close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not finish after cancellation")
	}

	if !strings.Contains(out.String(), `"id":7`) {
		t.Errorf("expected a response for the cancelled request, got: %s", out.String())
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type pipeWriter struct {
	ch chan []byte
}

func (w *pipeWriter) write(s string) { w.ch <- []byte(s) }
func (w *pipeWriter) close()         { close(w.ch) }

type pipeReader struct {
	ch      chan []byte
	pending []byte
	done    bool
}

func (r *pipeReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, errEOF
		}
		chunk, ok := <-r.ch
		if !ok {
			r.done = true
			return 0, errEOF
		}
		r.pending = chunk
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func newPipe() (*pipeReader, *pipeWriter) {
	ch := make(chan []byte, 16)
	return &pipeReader{ch: ch}, &pipeWriter{ch: ch}
}

var errEOF = io.EOF
