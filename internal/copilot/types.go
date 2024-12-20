// internal/copilot/types.go
package copilot

// Message represents a single message in the conversation
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest represents the request structure for the Copilot API
type CompletionRequest struct {
	Intent      bool      `json:"intent"`
	Model       string    `json:"model"`
	N           int       `json:"n"`
	Stream      bool      `json:"stream"`
	Temperature float32   `json:"temperature"`
	TopP        int       `json:"top_p"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
}

// Choice represents a single completion choice in the response
type Choice struct {
	Index   int `json:"index"`
	Message struct {
		Content string `json:"content"`
		Role    string `json:"role"`
	} `json:"message"`
	Delta struct {
		Content interface{} `json:"content"`
		Role    interface{} `json:"role"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

// CompletionResponse represents the response structure from the Copilot API
type CompletionResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewCompletionRequest creates a default completion request with standard parameters
func NewCompletionRequest(model string) CompletionRequest {
	return CompletionRequest{
		Intent:      false,
		Model:       model,
		N:           1,
		Stream:      false,
		Temperature: 0,
		TopP:        1,
		MaxTokens:   8192,
		Messages:    []Message{}, // Empty slice, will be filled by caller
	}
}
