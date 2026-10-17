// Package mcp implements the Model Context Protocol over stdio as JSON-RPC 2.0.
// Only the protocol revisions listed in SupportedVersions are negotiated, and
// the server advertises only the capabilities it actually implements.
package mcp

import "encoding/json"

// JSONRPCVersion is the JSON-RPC version every message carries.
const JSONRPCVersion = "2.0"

const (
	Version20241105  = "2024-11-05"
	Version20250326  = "2025-03-26"
	Version20250618  = "2025-06-18"
	Version20251125  = "2025-11-25"
	LatestMCPVersion = Version20251125
)

// SupportedVersions lists the published MCP revisions this server speaks,
// newest first.
var SupportedVersions = []string{
	Version20251125,
	Version20250618,
	Version20250326,
	Version20241105,
}

// NegotiateProtocolVersion returns the client's revision when it is supported,
// and the newest supported revision otherwise, as the specification requires.
func NegotiateProtocolVersion(requested string) string {
	if requested == "" {
		return LatestMCPVersion
	}
	for _, v := range SupportedVersions {
		if v == requested {
			return v
		}
	}
	return LatestMCPVersion
}

// IsSupportedVersion reports whether a protocol revision is one this server speaks.
func IsSupportedVersion(version string) bool {
	for _, v := range SupportedVersions {
		if v == version {
			return true
		}
	}
	return false
}

// Request is an incoming JSON-RPC request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`

	rawID json.RawMessage
}

// Response is an outgoing JSON-RPC result or error.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// Notification is an outgoing message that expects no reply.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC error object.
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

// NewRPCError builds an error object, omitting data when none is supplied.
func NewRPCError(code int, message string, data any) *RPCError {
	return &RPCError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// InitializeParams is what a client sends to open a session.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ClientCaps     `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

// ClientCaps are the capabilities a client declares at initialization.
type ClientCaps struct {
	Roots        *RootsCapability    `json:"roots,omitempty"`
	Sampling     *SamplingCapability `json:"sampling,omitempty"`
	Experimental map[string]any      `json:"experimental,omitempty"`
}

// RootsCapability declares client support for workspace roots.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability declares client support for model sampling.
type SamplingCapability struct{}

// Implementation names a peer and its version.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's reply to initialize.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ServerCaps     `json:"capabilities"`
	ServerInfo      Implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

// ServerCaps are the capabilities this server advertises. A field is set only
// when the behaviour behind it is implemented.
type ServerCaps struct {
	Tools       *ToolsCapability       `json:"tools,omitempty"`
	Prompts     *PromptsCapability     `json:"prompts,omitempty"`
	Resources   *ResourcesCapability   `json:"resources,omitempty"`
	Logging     *LoggingCapability     `json:"logging,omitempty"`
	Completions *CompletionsCapability `json:"completions,omitempty"`
}

// ToolsCapability declares tool support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability declares prompt support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability declares resource support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// LoggingCapability declares that the client may set a log level.
type LoggingCapability struct{}

// Tool is one callable tool with the JSON Schema describing its arguments.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

// ToolSchema is the JSON Schema object for a tool's arguments.
type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef describes one argument: its type and what it is for.
type PropertyDef struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Items       *PropertyDef           `json:"items,omitempty"`
	Enum        []string               `json:"enum,omitempty"`
	Properties  map[string]PropertyDef `json:"properties,omitempty"`
	Default     any                    `json:"default,omitempty"`
}

// ListToolsResult is the reply to tools/list, sorted by name so a client can
// cache it.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams names the tool to run and the arguments to run it with.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult carries a tool's output. IsError marks a failure the model
// should read rather than a protocol error.
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content is one piece of tool or prompt output.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NewTextContent wraps a string as text content.
func NewTextContent(text string) Content {
	return Content{
		Type: "text",
		Text: text,
	}
}

// Prompt is a reusable instruction template with its arguments.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is one argument a prompt accepts.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ListPromptsResult is the reply to prompts/list.
type ListPromptsResult struct {
	Prompts []Prompt `json:"prompts"`
}

// GetPromptParams names the prompt to render and its argument values.
type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptMessage is one message in a rendered prompt.
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// GetPromptResult is a rendered prompt ready for the model.
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// Resource is a readable document the server exposes by URI.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ListResourcesResult is the reply to resources/list.
type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
}

// ReadResourceParams names the resource URI to read.
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ResourceContent is the body of one resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ReadResourceResult is the reply to resources/read.
type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

// CompletionsCapability declares argument completion support.
type CompletionsCapability struct{}

// CompleteParams asks for completions for one argument being typed.
type CompleteParams struct {
	Ref      CompletionRef `json:"ref"`
	Argument ArgumentRef   `json:"argument"`
}

// CompletionRef names what the completion is for, a tool or a prompt.
type CompletionRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// ArgumentRef is the argument being completed and what has been typed so far.
type ArgumentRef struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CompleteResult carries the suggested values.
type CompleteResult struct {
	Completion CompletionDetails `json:"completion"`
}

// CompletionDetails holds the candidate values and how many there are.
type CompletionDetails struct {
	Values  []string `json:"values"`
	Total   int      `json:"total"`
	HasMore bool     `json:"hasMore"`
}

// SubscribeParams names a resource URI to watch.
type SubscribeParams struct {
	URI string `json:"uri"`
}

// SetLogLevelParams sets the minimum level the server will report.
type SetLogLevelParams struct {
	Level string `json:"level"`
}
