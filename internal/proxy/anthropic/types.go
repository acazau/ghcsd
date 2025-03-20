// internal/proxy/anthropic/types.go
package anthropic

// Anthropic API types for request/response handling
type ContentBlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ContentBlockImage struct {
	Type   string            `json:"type"`
	Source map[string]string `json:"source"`
}

type ContentBlockToolUse struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ContentBlockToolResult struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
}

type SystemContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Message represents a chat message
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// Tool represents a tool that can be used by the model
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"parameters"`
}

// ToolChoice represents how tools should be selected
type ToolChoice struct {
	Type string `json:"type"`
}

// Request represents an Anthropic API request
type Request struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	Messages   []Message   `json:"messages"`
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`
	System     string      `json:"system,omitempty"`
	Stream     bool        `json:"stream,omitempty"`
}

// Response represents an Anthropic API response
type Response struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Content      []Content `json:"content"`
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason"`
	SystemPrompt string    `json:"system"`
}

// Content represents a block of content in a message
type Content struct {
	Type      string     `json:"type"`
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool invocation
type ToolCall struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

// TokenCountRequest represents a request to count tokens
type TokenCountRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	System   string    `json:"system,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// TokenCountResponse represents the response from a token count request
type TokenCountResponse struct {
	InputTokens int `json:"input_tokens"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ThinkingConfig struct {
	Enabled bool `json:"enabled"`
}

// Colors for console output formatting
type Colors struct {
	Cyan      string
	Blue      string
	Green     string
	Yellow    string
	Red       string
	Magenta   string
	Reset     string
	Bold      string
	Underline string
	Dim       string
}

// ANSI color codes
var colors = Colors{
	Cyan:      "\033[96m",
	Blue:      "\033[94m",
	Green:     "\033[92m",
	Yellow:    "\033[93m",
	Red:       "\033[91m",
	Magenta:   "\033[95m",
	Reset:     "\033[0m",
	Bold:      "\033[1m",
	Underline: "\033[4m",
	Dim:       "\033[2m",
}
