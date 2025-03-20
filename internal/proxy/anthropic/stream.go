// internal/proxy/anthropic/stream.go
package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/temp/ghcsd/internal/copilot"
)

// StreamProcessor handles streaming responses
type StreamProcessor struct {
	debug   bool
	handler *Handler
	model   string
	flusher http.Flusher
	writer  http.ResponseWriter
}

// ProcessStream converts Copilot streaming format to Anthropic streaming format
// or passes through native Anthropic format events
func (p *StreamProcessor) ProcessStream(responseBody io.ReadCloser) error {
	defer responseBody.Close()

	reader := bufio.NewReader(responseBody)
	var isAnthropicFormat bool
	var firstLine string

	// Read the first line to determine the format
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil // Empty response
			}
			return fmt.Errorf("error reading stream: %v", err)
		}

		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Parse SSE data format
		if strings.HasPrefix(line, "data: ") {
			data := bytes.TrimPrefix([]byte(line), []byte("data: "))
			if bytes.Equal(data, []byte("[DONE]")) {
				// End of stream
				fmt.Fprint(p.writer, "data: [DONE]\n\n")
				p.flusher.Flush()
				return nil
			}

			// Try to parse as Anthropic format first
			var event map[string]interface{}
			if err := json.Unmarshal(data, &event); err == nil {
				if _, ok := event["type"].(string); ok {
					// This is Anthropic format - just pass it through
					isAnthropicFormat = true
					firstLine = line
					break
				}
			}

			// Not Anthropic format, must be Copilot format
			isAnthropicFormat = false
			firstLine = line
			break
		}
	}

	// Process the first line we already read
	if err := p.processLine(firstLine, isAnthropicFormat); err != nil {
		return err
	}

	// Process the rest of the stream
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading stream: %v", err)
		}

		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if err := p.processLine(line, isAnthropicFormat); err != nil {
			return err
		}
	}

	return nil
}

// processLine handles a single line of SSE data
func (p *StreamProcessor) processLine(line string, isAnthropicFormat bool) error {
	// Parse SSE data format
	if !strings.HasPrefix(line, "data: ") {
		return nil // Not SSE data
	}

	data := bytes.TrimPrefix([]byte(line), []byte("data: "))
	if bytes.Equal(data, []byte("[DONE]")) {
		// End of stream
		fmt.Fprint(p.writer, "data: [DONE]\n\n")
		p.flusher.Flush()
		return nil
	}

	if isAnthropicFormat {
		// Direct pass-through of Anthropic format
		fmt.Fprintf(p.writer, "%s\n\n", line)
		p.flusher.Flush()

		// Log if in debug mode
		if p.debug {
			var event map[string]interface{}
			if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("data: ")), &event); err == nil {
				if eventType, ok := event["type"].(string); ok {
					p.handler.logWithPrefix("Stream Event", fmt.Sprintf("%s: %s", eventType, string(data)))
				}
			}
		}
		return nil
	}

	// Handle Copilot format
	return p.processCopilotFormat(data)
}

// processCopilotFormat processes data in Copilot format and converts it to Anthropic format
func (p *StreamProcessor) processCopilotFormat(data []byte) error {
	var completionResp copilot.CompletionResponse

	if err := json.Unmarshal(data, &completionResp); err != nil {
		p.handler.logWithPrefix("Error", fmt.Sprintf("Failed to parse stream chunk: %v", err))
		return nil // Skip this chunk
	}

	// Extract usage info if available
	outputTokens := 0
	if completionResp.Usage.PromptTokens > 0 {
		outputTokens = completionResp.Usage.PromptTokens
	}
	if completionResp.Usage.CompletionTokens > 0 {
		outputTokens = completionResp.Usage.CompletionTokens
	}

	if len(completionResp.Choices) > 0 {
		choice := completionResp.Choices[0]

		// Get delta content
		deltaContent := ""
		if choice.Delta.Content != nil {
			if str, ok := choice.Delta.Content.(string); ok {
				deltaContent = str
			}
		}

		// Send text delta if there's content
		if deltaContent != "" {
			textDelta := map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": deltaContent,
				},
			}
			if err := p.sendEvent("content_block_delta", textDelta); err != nil {
				return err
			}
		}

		// Handle finish reason
		if choice.FinishReason != "" {
			// Close the text block
			contentBlockStop := map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			}
			if err := p.sendEvent("content_block_stop", contentBlockStop); err != nil {
				return err
			}

			// Map finish reason to stop reason
			stopReason := "end_turn"
			if choice.FinishReason == "length" {
				stopReason = "max_tokens"
			} else if choice.FinishReason == "tool_calls" {
				stopReason = "tool_use"
			}

			// Send message_delta with stop reason
			messageDelta := map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   stopReason,
					"stop_sequence": nil,
				},
				"usage": map[string]int{
					"output_tokens": outputTokens,
				},
			}
			if err := p.sendEvent("message_delta", messageDelta); err != nil {
				return err
			}

			// Send message_stop event
			messageStop := map[string]interface{}{
				"type": "message_stop",
			}
			if err := p.sendEvent("message_stop", messageStop); err != nil {
				return err
			}
		}
	}

	return nil
}

// sendEvent sends an SSE event with the given type and data
func (p *StreamProcessor) sendEvent(eventType string, data interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(p.writer, "event: %s\ndata: %s\n\n", eventType, dataBytes)
	p.flusher.Flush()
	if p.debug {
		p.handler.logWithPrefix("Stream Event", fmt.Sprintf("%s: %s", eventType, string(dataBytes)))
	}
	return nil
}
