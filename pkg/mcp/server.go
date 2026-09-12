package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

type ToolHandler func(ctx context.Context, args map[string]any) (*CallToolResult, error)

type PromptHandler func(ctx context.Context, args map[string]string) (*GetPromptResult, error)

type ResourceHandler func(ctx context.Context, uri string) (*ReadResourceResult, error)

type Server struct {
	name         string
	version      string
	instructions string

	tools            map[string]Tool
	toolHandlers     map[string]ToolHandler
	prompts          map[string]Prompt
	promptHandlers   map[string]PromptHandler
	resources        map[string]Resource
	resourceHandlers map[string]ResourceHandler
	subscriptions    map[string]bool
	logLevel         string

	in     io.Reader
	out    io.Writer
	logger *log.Logger

	writeMu     sync.Mutex
	initialized bool
	mu          sync.RWMutex
}

type Option func(*Server)

func WithIO(in io.Reader, out io.Writer) Option {
	return func(s *Server) {
		s.in = in
		s.out = out
	}
}

func WithLogger(logger *log.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

func WithInstructions(instructions string) Option {
	return func(s *Server) {
		s.instructions = instructions
	}
}

func NewServer(name, version string, opts ...Option) *Server {
	s := &Server{
		name:             name,
		version:          version,
		tools:            make(map[string]Tool),
		toolHandlers:     make(map[string]ToolHandler),
		prompts:          make(map[string]Prompt),
		promptHandlers:   make(map[string]PromptHandler),
		resources:        make(map[string]Resource),
		resourceHandlers: make(map[string]ResourceHandler),
		subscriptions:    make(map[string]bool),
		logLevel:         "info",
		in:               os.Stdin,
		out:              os.Stdout,
		logger:           log.New(os.Stderr, fmt.Sprintf("[%s] ", name), log.LstdFlags),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.toolHandlers[tool.Name] = handler
}

func (s *Server) RegisterPrompt(prompt Prompt, handler PromptHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
	s.promptHandlers[prompt.Name] = handler
}

func (s *Server) RegisterResource(resource Resource, handler ResourceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = resource
	s.resourceHandlers[resource.URI] = handler
}

func (s *Server) Log(format string, v ...any) {
	if s.logger != nil {
		s.logger.Printf(format, v...)
	}
}

func (s *Server) Serve(ctx context.Context) error {
	s.Log("MCP Server started (%s v%s)", s.name, s.version)

	scanner := bufio.NewScanner(s.in)

	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read error: %w", err)
			}
			return nil
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		s.handleRawMessage(ctx, line)
	}
}

func (s *Server) handleRawMessage(ctx context.Context, raw []byte) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		s.sendError(nil, ErrCodeParseError, "Parse error: invalid JSON", err.Error())
		return
	}

	if req.ID == nil {
		s.handleNotification(ctx, &req)
		return
	}

	s.handleRequest(ctx, &req)
}

func (s *Server) handleNotification(_ context.Context, req *Request) {
	switch req.Method {
	case "notifications/initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
		s.Log("Client confirmed initialization")
	case "notifications/cancelled":
		s.Log("Client cancelled request: %s", string(req.Params))
	default:
		s.Log("Ignored notification: %s", req.Method)
	}
}

func (s *Server) handleRequest(ctx context.Context, req *Request) {
	switch req.Method {
	case "initialize":
		var params InitializeParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		protocolVersion := NegotiateProtocolVersion(params.ProtocolVersion)
		s.Log("Client connected: %s (version: %s, negotiated protocol: %s)", params.ClientInfo.Name, params.ClientInfo.Version, protocolVersion)

		result := InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: ServerCaps{
				Tools:       &ToolsCapability{ListChanged: true},
				Prompts:     &PromptsCapability{ListChanged: true},
				Resources:   &ResourcesCapability{Subscribe: true, ListChanged: true},
				Logging:     &LoggingCapability{},
				Completions: &CompletionsCapability{},
			},
			ServerInfo: Implementation{
				Name:    s.name,
				Version: s.version,
			},
			Instructions: s.instructions,
		}
		s.sendResult(req.ID, result)

	case "ping":
		s.sendResult(req.ID, map[string]any{})

	case "tools/list":
		s.mu.RLock()
		toolsList := make([]Tool, 0, len(s.tools))
		for _, t := range s.tools {
			toolsList = append(toolsList, t)
		}
		s.mu.RUnlock()

		s.sendResult(req.ID, ListToolsResult{Tools: toolsList})

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrCodeInvalidParams, "Invalid params for tools/call", err.Error())
			return
		}

		s.mu.RLock()
		handler, exists := s.toolHandlers[params.Name]
		s.mu.RUnlock()

		if !exists {
			s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Tool not found: %s", params.Name), nil)
			return
		}

		s.Log("Executing tool: %s", params.Name)
		result, err := executeToolSafely(handler, ctx, params.Arguments)
		if err != nil {
			s.Log("Tool %s error: %v", params.Name, err)
			s.sendResult(req.ID, CallToolResult{
				Content: []Content{NewTextContent(fmt.Sprintf("Error: %v", err))},
				IsError: true,
			})
			return
		}

		s.sendResult(req.ID, result)

	case "prompts/list":
		s.mu.RLock()
		promptsList := make([]Prompt, 0, len(s.prompts))
		for _, p := range s.prompts {
			promptsList = append(promptsList, p)
		}
		s.mu.RUnlock()

		s.sendResult(req.ID, ListPromptsResult{Prompts: promptsList})

	case "prompts/get":
		var params GetPromptParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrCodeInvalidParams, "Invalid params for prompts/get", err.Error())
			return
		}

		s.mu.RLock()
		handler, exists := s.promptHandlers[params.Name]
		s.mu.RUnlock()

		if !exists {
			s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Prompt not found: %s", params.Name), nil)
			return
		}

		result, err := executePromptSafely(handler, ctx, params.Arguments)
		if err != nil {
			s.sendError(req.ID, ErrCodeInternal, err.Error(), nil)
			return
		}
		s.sendResult(req.ID, result)

	case "resources/list":
		s.mu.RLock()
		resourcesList := make([]Resource, 0, len(s.resources))
		for _, r := range s.resources {
			resourcesList = append(resourcesList, r)
		}
		s.mu.RUnlock()

		s.sendResult(req.ID, ListResourcesResult{Resources: resourcesList})

	case "resources/read":
		var params ReadResourceParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrCodeInvalidParams, "Invalid params for resources/read", err.Error())
			return
		}

		s.mu.RLock()
		handler, exists := s.resourceHandlers[params.URI]
		s.mu.RUnlock()

		if !exists {
			s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Resource not found: %s", params.URI), nil)
			return
		}

		result, err := executeResourceSafely(handler, ctx, params.URI)
		if err != nil {
			s.sendError(req.ID, ErrCodeInternal, err.Error(), nil)
			return
		}
		s.sendResult(req.ID, result)

	case "completion/complete":
		var params CompleteParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrCodeInvalidParams, "Invalid params for completion/complete", err.Error())
			return
		}

		s.mu.RLock()
		completions := s.generateCompletions(params)
		s.mu.RUnlock()

		s.sendResult(req.ID, CompleteResult{
			Completion: CompletionDetails{
				Values:  completions,
				Total:   len(completions),
				HasMore: false,
			},
		})

	case "resources/subscribe":
		var params SubscribeParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		s.mu.Lock()
		s.subscriptions[params.URI] = true
		s.mu.Unlock()
		s.Log("Client subscribed to resource: %s", params.URI)
		s.sendResult(req.ID, map[string]any{})

	case "resources/unsubscribe":
		var params SubscribeParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		s.mu.Lock()
		delete(s.subscriptions, params.URI)
		s.mu.Unlock()
		s.Log("Client unsubscribed from resource: %s", params.URI)
		s.sendResult(req.ID, map[string]any{})

	case "logging/setLevel":
		var params SetLogLevelParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		s.mu.Lock()
		s.logLevel = params.Level
		s.mu.Unlock()
		s.Log("Log level set to: %s", params.Level)
		s.sendResult(req.ID, map[string]any{})

	default:
		s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Method not supported: %s", req.Method), nil)
	}
}

func (s *Server) sendResult(id any, result any) {
	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
	s.writeJSON(resp)
}

func (s *Server) sendError(id any, code int, message string, data any) {
	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   NewRPCError(code, message, data),
	}
	s.writeJSON(resp)
}

func (s *Server) writeJSON(v any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		s.Log("Error marshaling response: %v", err)
		return
	}

	_, _ = s.out.Write(data)
	_, _ = s.out.Write([]byte("\n"))
}

func executeToolSafely(handler ToolHandler, ctx context.Context, args map[string]any) (res *CallToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in tool handler: %v", r)
		}
	}()
	return handler(ctx, args)
}

func executePromptSafely(handler PromptHandler, ctx context.Context, args map[string]string) (res *GetPromptResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in prompt handler: %v", r)
		}
	}()
	return handler(ctx, args)
}

func executeResourceSafely(handler ResourceHandler, ctx context.Context, uri string) (res *ReadResourceResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in resource handler: %v", r)
		}
	}()
	return handler(ctx, uri)
}

func (s *Server) generateCompletions(params CompleteParams) []string {
	var candidates []string
	val := strings.ToLower(params.Argument.Value)

	if params.Ref.Type == "ref/tool" {
		switch params.Argument.Name {
		case "connection":
			candidates = []string{"default", "postgres", "mysql", "sqlite"}
		case "package":
			candidates = []string{"./...", "./cmd/...", "./pkg/...", "./internal/..."}
		case "path":
			candidates = []string{"./internal/domain", "./internal/repository", "./internal/service", "./internal/handler", "./pkg", "./cmd"}
		case "symbol":
			candidates = []string{"net/http.Client", "context.Context", "sync.Mutex", "fmt.Sprintf", "database/sql.DB", "log/slog.Info"}
		}
	} else if params.Ref.Type == "ref/prompt" {
		candidates = []string{"symbol", "file", "feature", "package"}
	}

	var results []string
	cleanVal := strings.TrimPrefix(val, "./")
	for _, c := range candidates {
		cleanC := strings.TrimPrefix(strings.ToLower(c), "./")
		if strings.HasPrefix(cleanC, cleanVal) || strings.Contains(strings.ToLower(c), val) || val == "" {
			results = append(results, c)
		}
	}
	return results
}

func (s *Server) SendNotification(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = data
	}

	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  raw,
	}

	s.writeJSON(notif)
	return nil
}

func (s *Server) NotifyToolsListChanged() error {
	return s.SendNotification("notifications/tools/list_changed", map[string]any{})
}

func (s *Server) NotifyResourceUpdated(uri string) error {
	return s.SendNotification("notifications/resources/updated", map[string]string{"uri": uri})
}
