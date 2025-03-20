// internal/proxy/anthropic/conversion.go
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
