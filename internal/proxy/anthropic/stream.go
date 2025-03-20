// internal/proxy/anthropic/stream.go
package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
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
func (p *StreamProcessor) ProcessStream(responseBody io.ReadCloser) error {
	defer responseBody.Close()
	messageID := fmt.Sprintf("msg_%s", uuid.New().String())

	// Start the message
	messageStart := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":          messageID,
			"type":        "message",
			"role":        "assistant",
			"model":       p.model,
			"content":     []interface{}{},
			"stop_reason": nil,
			"usage": map[string]int{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	if err := p.sendEvent("message_start", messageStart); err != nil {
		return err
	}

	// Start the first content block (text)
	contentBlockStart := map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	if err := p.sendEvent("content_block_start", contentBlockStart); err != nil {
		return err
	}

	// Send a ping to keep the connection alive
	ping := map[string]interface{}{
		"type": "ping",
	}
	if err := p.sendEvent("ping", ping); err != nil {
		return err
	}

	buffer := bufio.NewReader(responseBody)

	// Track state for better streaming
	toolIndex := -1
	accumulatedText := ""
	textSent := false
	textBlockClosed := false
	outputTokens := 0
	hasSentStopReason := false
	lastToolIndex := 0

	for {
		line, err := buffer.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("error reading stream: %v", err)
			}
			break
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Parse SSE data format
		if bytes.HasPrefix(line, []byte("data: ")) {
			line = bytes.TrimPrefix(line, []byte("data: "))
			if bytes.Equal(line, []byte("[DONE]")) {
				// End of stream
				break
			}

			var completionResp copilot.CompletionResponse
			if err := json.Unmarshal(line, &completionResp); err != nil {
				p.handler.logWithPrefix("Error", fmt.Sprintf("Failed to parse stream chunk: %v", err))
				continue
			}

			// Extract usage info if available
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

				// Accumulate text content
				if deltaContent != "" {
					accumulatedText += deltaContent
					// Send text deltas if no tool calls have started
					if toolIndex == -1 && !textBlockClosed {
						textSent = true
						if err := p.sendEvent("content_block_delta", map[string]interface{}{
							"type":  "content_block_delta",
							"index": 0,
							"delta": map[string]interface{}{
								"type": "text_delta",
								"text": deltaContent,
							},
						}); err != nil {
							return err
						}
					}
				}

				// Handle tool calls - currently not part of Copilot API but implemented for future compatibility

				// Check if this is the final message
				if choice.FinishReason != "" && !hasSentStopReason {
					hasSentStopReason = true

					// Close any open tool blocks
					if toolIndex != -1 {
						for i := 1; i <= lastToolIndex; i++ {
							if err := p.sendEvent("content_block_stop", map[string]interface{}{
								"type":  "content_block_stop",
								"index": i,
							}); err != nil {
								return err
							}
						}
					}

					// If we accumulated text but never sent or closed text block, do it now
					if !textBlockClosed {
						if accumulatedText != "" && !textSent {
							// Send the accumulated text
							if err := p.sendEvent("content_block_delta", map[string]interface{}{
								"type":  "content_block_delta",
								"index": 0,
								"delta": map[string]interface{}{
									"type": "text_delta",
									"text": accumulatedText,
								},
							}); err != nil {
								return err
							}
						}

						// Close the text block
						if err := p.sendEvent("content_block_stop", map[string]interface{}{
							"type":  "content_block_stop",
							"index": 0,
						}); err != nil {
							return err
						}
						textBlockClosed = true
					}

					// Map finish reason to stop reason
					stopReason := "end_turn"
					if choice.FinishReason == "length" {
						stopReason = "max_tokens"
					} else if choice.FinishReason == "tool_calls" {
						stopReason = "tool_use"
					}

					// Send message_delta with stop reason
					if err := p.sendEvent("message_delta", map[string]interface{}{
						"type": "message_delta",
						"delta": map[string]interface{}{
							"stop_reason":   stopReason,
							"stop_sequence": nil,
						},
						"usage": map[string]int{
							"output_tokens": outputTokens,
						},
					}); err != nil {
						return err
					}

					// Send message_stop event
					if err := p.sendEvent("message_stop", map[string]interface{}{
						"type": "message_stop",
					}); err != nil {
						return err
					}

					// Send final [DONE] marker
					fmt.Fprint(p.writer, "data: [DONE]\n\n")
					p.flusher.Flush()
					break
				}
			}
		}
	}

	// If we didn't get a finish reason, close any open blocks
	if !hasSentStopReason {
		// Close any open tool call blocks
		if toolIndex != -1 {
			for i := 1; i <= lastToolIndex; i++ {
				if err := p.sendEvent("content_block_stop", map[string]interface{}{
					"type":  "content_block_stop",
					"index": i,
				}); err != nil {
					return err
				}
			}
		}

		// Close the text content block
		if !textBlockClosed {
			if err := p.sendEvent("content_block_stop", map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			}); err != nil {
				return err
			}
			textBlockClosed = true
		}

		// Send final message_delta with usage
		if err := p.sendEvent("message_delta", map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]int{
				"output_tokens": outputTokens,
			},
		}); err != nil {
			return err
		}

		// Send message_stop event
		if err := p.sendEvent("message_stop", map[string]interface{}{
			"type": "message_stop",
		}); err != nil {
			return err
		}

		// Send final [DONE] marker
		fmt.Fprint(p.writer, "data: [DONE]\n\n")
		p.flusher.Flush()
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
