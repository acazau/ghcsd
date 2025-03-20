// internal/proxy/anthropic/conversion.go
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/temp/ghcsd/internal/copilot"
)

// convertAnthropicToCopilotMessages converts Anthropic API messages to Copilot format
func (h *Handler) convertAnthropicToCopilotMessages(req Request) ([]copilot.Message, error) {
	var copilotMessages []copilot.Message

	// Handle system message if present
	if req.System != "" {
		copilotMessages = append(copilotMessages, copilot.Message{
			Role:    "system",
			Content: req.System,
		})
	}

	// Handle tools and tool_choice - add to system message
	if len(req.Tools) > 0 {
		toolsMsg := "Available Tools:\n"
		for _, tool := range req.Tools {
			toolDescription := tool.Description
			if toolDescription == "" {
				toolDescription = fmt.Sprintf("Tool for %s operations", tool.Name)
			}
			toolInputSchema, err := json.Marshal(tool.InputSchema)
			if err != nil {
				toolInputSchema = []byte("{}")
			}
			toolsMsg += fmt.Sprintf("- %s: %s\nInput Schema: %s\n\n",
				tool.Name,
				toolDescription,
				string(toolInputSchema))
		}

		// Add tool choice information if present
		if req.ToolChoice != nil {
			if req.ToolChoice.Type == "auto" || req.ToolChoice.Type == "any" {
				toolsMsg += fmt.Sprintf("Tool Choice: %s\n", req.ToolChoice.Type)
			}
		}

		// Add tool information to system message or create a new system message
		toolsSystemMessage := copilot.Message{
			Role:    "system",
			Content: toolsMsg,
		}

		// Check if we already have a system message to append to
		if len(copilotMessages) > 0 && copilotMessages[0].Role == "system" {
			// Append to existing system message
			existingContent := copilotMessages[0].GetStringContent()
			copilotMessages[0].Content = existingContent + "\n\n" + toolsMsg
		} else {
			// Add as new system message
			copilotMessages = append([]copilot.Message{toolsSystemMessage}, copilotMessages...)
		}
	}

	// Process conversation messages with improved tool result handling
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
			// Simple string content
			copilotMessages = append(copilotMessages, copilot.Message{
				Role:    msg.Role,
				Content: content,
			})
		case []interface{}:
			// Content blocks
			var messageText strings.Builder
			hasToolResult := false

			// First pass: Check if we have any tool_result blocks
			for _, block := range content {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockType, ok := blockMap["type"].(string); ok && blockType == "tool_result" {
						hasToolResult = true
						break
					}
				}
			}

			// If we have tool results, we need to handle them specially (similar to Python version)
			if hasToolResult && msg.Role == "user" {
				// Similar to Python version's special handling for tool_result in user messages
				var textContent string
				for _, block := range content {
					blockMap, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					blockType, _ := blockMap["type"].(string)
					switch blockType {
					case "text":
						if text, ok := blockMap["text"].(string); ok {
							textContent += text + "\n"
						}
					case "tool_result":
						toolUseID, _ := blockMap["tool_use_id"].(string)
						resultContent := blockMap["content"]
						parsedContent := h.ParseToolResultContent(resultContent)
						textContent += fmt.Sprintf("Tool result for %s:\n%s\n\n", toolUseID, parsedContent)
					}
				}

				// Add combined message with tool results
				if textContent != "" {
					copilotMessages = append(copilotMessages, copilot.Message{
						Role:    msg.Role,
						Content: textContent,
					})
				}
			} else {
				// Handle regular message with multiple content blocks
				for _, block := range content {
					blockMap, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					blockType, _ := blockMap["type"].(string)
					switch blockType {
					case "text":
						if text, ok := blockMap["text"].(string); ok {
							messageText.WriteString(text)
							messageText.WriteString("\n")
						}
					case "image":
						messageText.WriteString("[Image content not displayed]\n")
					case "tool_use":
						name, _ := blockMap["name"].(string)
						id, _ := blockMap["id"].(string)
						input, _ := blockMap["input"].(map[string]interface{})
						inputJSON, _ := json.Marshal(input)
						messageText.WriteString(fmt.Sprintf("[Tool: %s (ID: %s)]\nInput: %s\n\n",
							name, id, string(inputJSON)))
					case "tool_result":
						toolUseID, _ := blockMap["tool_use_id"].(string)
						resultContent := blockMap["content"]
						// Use our helper function to parse the tool result content
						parsedContent := h.ParseToolResultContent(resultContent)
						messageText.WriteString(fmt.Sprintf("Tool result for %s:\n%s\n\n", toolUseID, parsedContent))
					}
				}

				// Add the compiled message
				if messageText.Len() > 0 {
					copilotMessages = append(copilotMessages, copilot.Message{
						Role:    msg.Role,
						Content: messageText.String(),
					})
				}
			}
		default:
			return nil, fmt.Errorf("unsupported message content format")
		}
	}

	return copilotMessages, nil
}

// convertCopilotToAnthropicResponse converts a Copilot response to Anthropic format
func (h *Handler) convertCopilotToAnthropicResponse(response *copilot.CompletionResponse, model string) *Response {
	// Create Anthropic-style content blocks
	var contents []Content

	// Get the assistant's response text
	if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
		contents = append(contents, Content{
			Type: "text",
			Text: response.Choices[0].Message.Content,
		})
	}

	// Map finish reason to Anthropic stop reason
	stopReason := "end_turn"
	if len(response.Choices) > 0 {
		switch response.Choices[0].FinishReason {
		case "length":
			stopReason = "max_tokens"
		case "tool_calls":
			stopReason = "tool_use"
		}
	}

	// Create the response
	return &Response{
		ID:         fmt.Sprintf("msg_%s", uuid.New().String()),
		Model:      model,
		Role:       "assistant",
		Content:    contents,
		Type:       "message",
		StopReason: stopReason,
	}
}

// ParseToolResultContent extracts usable text from complex tool result content
func (h *Handler) ParseToolResultContent(content interface{}) string {
	if content == nil {
		return "No content provided"
	}

	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var result strings.Builder
		for _, item := range c {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemMap["type"] == "text" {
					if text, ok := itemMap["text"].(string); ok {
						result.WriteString(text + "\n")
					}
				} else if text, ok := itemMap["text"].(string); ok {
					result.WriteString(text + "\n")
				} else {
					// Try to convert the whole item to JSON
					if jsonStr, err := json.Marshal(itemMap); err == nil {
						result.WriteString(string(jsonStr) + "\n")
					} else {
						result.WriteString(fmt.Sprintf("%v\n", itemMap))
					}
				}
			} else if itemStr, ok := item.(string); ok {
				result.WriteString(itemStr + "\n")
			} else {
				// Try to convert the unknown item to string
				result.WriteString(fmt.Sprintf("%v\n", item))
			}
		}
		return strings.TrimSpace(result.String())
	case map[string]interface{}:
		// Handle dictionary content
		if c["type"] == "text" {
			if text, ok := c["text"].(string); ok {
				return text
			}
		}
		// Try to convert the whole map to JSON
		if jsonStr, err := json.Marshal(c); err == nil {
			return string(jsonStr)
		}
		return fmt.Sprintf("%v", c)
	default:
		// Fallback for any other type
		return fmt.Sprintf("%v", c)
	}
}
