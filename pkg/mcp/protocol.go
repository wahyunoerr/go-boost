package mcp

import "encoding/json"

const JSONRPCVersion = "2.0"

const (
	Version2024      = "2024-11-05"
	Version2025      = "2025-11-25"
	Version2026      = "2026-01-01"
	LatestMCPVersion = Version2026
)

var SupportedVersions = []string{
	Version2026,
	Version2025,
	Version2024,
}

func NegotiateProtocolVersion(requested string) string {
	if requested == "" {
		return LatestMCPVersion
	}
	for _, v := range SupportedVersions {
		if v == requested {
			return v
		}
	}
	if len(requested) >= 4 && requested[:4] == "2024" {
		return Version2024
	}
	return LatestMCPVersion
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

func NewRPCError(code int, message string, data any) *RPCError {
	return &RPCError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ClientCaps     `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

type ClientCaps struct {
	Roots        *RootsCapability    `json:"roots,omitempty"`
	Sampling     *SamplingCapability `json:"sampling,omitempty"`
	Experimental map[string]any      `json:"experimental,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ServerCaps     `json:"capabilities"`
	ServerInfo      Implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

type ServerCaps struct {
	Tools       *ToolsCapability       `json:"tools,omitempty"`
	Prompts     *PromptsCapability     `json:"prompts,omitempty"`
	Resources   *ResourcesCapability   `json:"resources,omitempty"`
	Logging     *LoggingCapability     `json:"logging,omitempty"`
	Completions *CompletionsCapability `json:"completions,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type PropertyDef struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Items       *PropertyDef           `json:"items,omitempty"`
	Enum        []string               `json:"enum,omitempty"`
	Properties  map[string]PropertyDef `json:"properties,omitempty"`
	Default     any                    `json:"default,omitempty"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func NewTextContent(text string) Content {
	return Content{
		Type: "text",
		Text: text,
	}
}

type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type ListPromptsResult struct {
	Prompts []Prompt `json:"prompts"`
}

type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
}

type ReadResourceParams struct {
	URI string `json:"uri"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

type CompletionsCapability struct{}

type CompleteParams struct {
	Ref      CompletionRef `json:"ref"`
	Argument ArgumentRef   `json:"argument"`
}

type CompletionRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type ArgumentRef struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CompleteResult struct {
	Completion CompletionDetails `json:"completion"`
}

type CompletionDetails struct {
	Values  []string `json:"values"`
	Total   int      `json:"total"`
	HasMore bool     `json:"hasMore"`
}

type SubscribeParams struct {
	URI string `json:"uri"`
}

type SetLogLevelParams struct {
	Level string `json:"level"`
}
