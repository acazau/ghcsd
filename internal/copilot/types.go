// internal/copilot/types.go
package copilot

// MessageContent represents a single content item in a message
type MessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Message represents a single message in the conversation
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []MessageContent
}

// StreamOptions represents streaming-specific configuration
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// CompletionRequest represents the request structure for the Copilot API
type CompletionRequest struct {
	Intent        bool           `json:"intent"`
	Model         string         `json:"model"`
	N             int            `json:"n"`
	Stream        bool           `json:"stream"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	Temperature   float32        `json:"temperature"`
	TopP          int            `json:"top_p"`
	Messages      []Message      `json:"messages"`
	MaxTokens     int            `json:"max_tokens"`
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
		MaxTokens:   32768,
		Messages:    []Message{}, // Empty slice, will be filled by caller
	}
}

// IsStringContent checks if the message content is a string
func (m *Message) IsStringContent() bool {
	_, ok := m.Content.(string)
	return ok
}

// GetStringContent returns the content as a string if it is one,
// otherwise returns an empty string
func (m *Message) GetStringContent() string {
	if str, ok := m.Content.(string); ok {
		return str
	}
	return ""
}

// GetComplexContent attempts to extract the complex content array
// returns nil if content is not of the expected type
func (m *Message) GetComplexContent() []MessageContent {
	if contentArr, ok := m.Content.([]MessageContent); ok {
		return contentArr
	}
	// Handle the case where content might be a []interface{} from JSON unmarshaling
	if contentArr, ok := m.Content.([]interface{}); ok {
		result := make([]MessageContent, 0, len(contentArr))
		for _, item := range contentArr {
			if mapItem, ok := item.(map[string]interface{}); ok {
				content := MessageContent{}
				if typeStr, ok := mapItem["type"].(string); ok {
					content.Type = typeStr
				}
				if text, ok := mapItem["text"].(string); ok {
					content.Text = text
				}
				result = append(result, content)
			}
		}
		return result
	}
	return nil
}
