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
	"sort"
	"strings"
	"sync"
)

const (
	maxMessageBytes    = 16 * 1024 * 1024
	maxConcurrentCalls = 8
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
	logLevel         string

	in     io.Reader
	out    io.Writer
	logger *log.Logger

	writeMu     sync.Mutex
	mu          sync.RWMutex
	initialized bool

	inflight   map[string]context.CancelFunc
	inflightMu sync.Mutex
	slots      chan struct{}
	wg         sync.WaitGroup

	completionSource CompletionSource
}

type CompletionSource interface {
	Connections() []string
	Interfaces() []string
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

func WithCompletionSource(source CompletionSource) Option {
	return func(s *Server) {
		s.completionSource = source
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
		logLevel:         "info",
		in:               os.Stdin,
		out:              os.Stdout,
		logger:           log.New(os.Stderr, fmt.Sprintf("[%s] ", name), log.LstdFlags),
		inflight:         make(map[string]context.CancelFunc),
		slots:            make(chan struct{}, maxConcurrentCalls),
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines := make(chan []byte)
	readErr := make(chan error, 1)

	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(s.in, 64*1024)
		for {
			line, err := readMessage(reader)
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					readErr <- err
				}
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				s.wg.Wait()
				select {
				case err := <-readErr:
					return fmt.Errorf("read error: %w", err)
				default:
					return nil
				}
			}
			s.handleRawMessage(ctx, line)
		}
	}
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 && buf.Len() < maxMessageBytes {
			buf.Write(chunk)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimSpace(buf.Bytes()), err
	}
}

func (s *Server) handleRawMessage(ctx context.Context, raw []byte) {
	if len(raw) == 0 {
		return
	}

	var probe struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		s.sendError(nil, ErrCodeParseError, "Parse error: invalid JSON", err.Error())
		return
	}

	if probe.Method == "" {
		if probe.ID != nil {
			s.sendError(rawID(probe.ID), ErrCodeInvalidRequest, "Invalid request: missing method", nil)
		}
		return
	}

	req := &Request{
		JSONRPC: probe.JSONRPC,
		Method:  probe.Method,
		Params:  probe.Params,
		ID:      rawID(probe.ID),
		rawID:   probe.ID,
	}

	if probe.ID == nil || string(probe.ID) == "null" {
		s.handleNotification(ctx, req)
		return
	}

	s.dispatchRequest(ctx, req)
}

func rawID(id json.RawMessage) any {
	if id == nil {
		return nil
	}
	var value any
	if err := json.Unmarshal(id, &value); err != nil {
		return nil
	}
	return value
}

func (s *Server) handleNotification(_ context.Context, req *Request) {
	switch req.Method {
	case "notifications/initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
		s.Log("Client confirmed initialization")
	case "notifications/cancelled":
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
			Reason    string          `json:"reason"`
		}
		if err := json.Unmarshal(req.Params, &params); err == nil {
			s.cancelRequest(string(params.RequestID))
			s.Log("Cancelled request %s: %s", string(params.RequestID), params.Reason)
		}
	default:
		s.Log("Ignored notification: %s", req.Method)
	}
}

func (s *Server) cancelRequest(id string) {
	s.inflightMu.Lock()
	cancel, ok := s.inflight[id]
	s.inflightMu.Unlock()
	if ok {
		cancel()
	}
}

func (s *Server) dispatchRequest(ctx context.Context, req *Request) {
	if req.Method == "initialize" || req.Method == "ping" {
		s.handleRequest(ctx, req)
		return
	}

	s.mu.RLock()
	ready := s.initialized
	s.mu.RUnlock()
	if !ready {
		s.Log("Request %s arrived before initialization completed", req.Method)
	}

	if !isLongRunning(req.Method) {
		s.handleRequest(ctx, req)
		return
	}

	callCtx, cancel := context.WithCancel(ctx)
	key := string(req.rawID)

	s.inflightMu.Lock()
	s.inflight[key] = cancel
	s.inflightMu.Unlock()

	s.wg.Add(1)
	s.slots <- struct{}{}
	go func() {
		defer func() {
			<-s.slots
			s.inflightMu.Lock()
			delete(s.inflight, key)
			s.inflightMu.Unlock()
			cancel()
			s.wg.Done()
		}()
		s.handleRequest(callCtx, req)
	}()
}

func isLongRunning(method string) bool {
	return method == "tools/call" || method == "resources/read"
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

		s.sendResult(req.ID, InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: ServerCaps{
				Tools:       &ToolsCapability{},
				Prompts:     &PromptsCapability{},
				Resources:   &ResourcesCapability{},
				Logging:     &LoggingCapability{},
				Completions: &CompletionsCapability{},
			},
			ServerInfo: Implementation{
				Name:    s.name,
				Version: s.version,
			},
			Instructions: s.instructions,
		})

	case "ping":
		s.sendResult(req.ID, map[string]any{})

	case "tools/list":
		s.mu.RLock()
		toolsList := make([]Tool, 0, len(s.tools))
		for _, t := range s.tools {
			toolsList = append(toolsList, t)
		}
		s.mu.RUnlock()

		sort.Slice(toolsList, func(i, j int) bool { return toolsList[i].Name < toolsList[j].Name })
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
		if result == nil {
			s.sendResult(req.ID, CallToolResult{Content: []Content{}})
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

		sort.Slice(promptsList, func(i, j int) bool { return promptsList[i].Name < promptsList[j].Name })
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

		sort.Slice(resourcesList, func(i, j int) bool { return resourcesList[i].URI < resourcesList[j].URI })
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

		completions := s.generateCompletions(params)
		s.sendResult(req.ID, CompleteResult{
			Completion: CompletionDetails{
				Values:  completions,
				Total:   len(completions),
				HasMore: false,
			},
		})

	case "logging/setLevel":
		var params SetLogLevelParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		if !isValidLogLevel(params.Level) {
			s.sendError(req.ID, ErrCodeInvalidParams, fmt.Sprintf("Unsupported log level: %s", params.Level), nil)
			return
		}
		s.mu.Lock()
		s.logLevel = strings.ToLower(params.Level)
		s.mu.Unlock()
		s.Log("Log level set to: %s", params.Level)
		s.sendResult(req.ID, map[string]any{})

	default:
		s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Method not supported: %s", req.Method), nil)
	}
}

var logLevelRank = map[string]int{
	"debug": 0, "info": 1, "notice": 2, "warning": 3,
	"error": 4, "critical": 5, "alert": 6, "emergency": 7,
}

func isValidLogLevel(level string) bool {
	_, ok := logLevelRank[strings.ToLower(level)]
	return ok
}

func (s *Server) LogMessage(level, message string) {
	s.mu.RLock()
	current := s.logLevel
	initialized := s.initialized
	s.mu.RUnlock()

	if !initialized {
		return
	}
	if logLevelRank[strings.ToLower(level)] < logLevelRank[current] {
		return
	}

	_ = s.SendNotification("notifications/message", map[string]any{
		"level":  strings.ToLower(level),
		"logger": s.name,
		"data":   message,
	})
}

func (s *Server) sendResult(id any, result any) {
	s.writeJSON(Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(id any, code int, message string, data any) {
	s.writeJSON(Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   NewRPCError(code, message, data),
	})
}

func (s *Server) writeJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		s.Log("Error marshaling response: %v", err)
		return
	}
	data = append(data, '\n')

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.out.Write(data)
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

	switch params.Ref.Type {
	case "ref/tool":
		switch params.Argument.Name {
		case "connection":
			candidates = s.connectionCandidates()
		case "package":
			candidates = []string{"./...", "./cmd/...", "./pkg/...", "./internal/..."}
		case "path":
			candidates = []string{"./internal/domain", "./internal/repository", "./internal/service", "./internal/handler", "./pkg", "./cmd"}
		case "symbol":
			candidates = []string{"net/http.Client", "context.Context", "sync.Mutex", "fmt.Sprintf", "database/sql.DB", "log/slog.Info"}
		case "interface":
			candidates = s.interfaceCandidates()
		}
	case "ref/prompt":
		candidates = []string{"symbol", "file", "feature", "package"}
	}

	results := make([]string, 0, len(candidates))
	cleanVal := strings.TrimPrefix(val, "./")
	for _, c := range candidates {
		cleanC := strings.TrimPrefix(strings.ToLower(c), "./")
		if val == "" || strings.HasPrefix(cleanC, cleanVal) || strings.Contains(strings.ToLower(c), val) {
			results = append(results, c)
		}
	}
	return results
}

func (s *Server) connectionCandidates() []string {
	if s.completionSource == nil {
		return []string{"default"}
	}
	return s.completionSource.Connections()
}

func (s *Server) interfaceCandidates() []string {
	if s.completionSource == nil {
		return nil
	}
	return s.completionSource.Interfaces()
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

	s.writeJSON(Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  raw,
	})
	return nil
}

func (s *Server) NotifyToolsListChanged() error {
	return s.SendNotification("notifications/tools/list_changed", map[string]any{})
}

func (s *Server) NotifyResourceUpdated(uri string) error {
	return s.SendNotification("notifications/resources/updated", map[string]string{"uri": uri})
}
