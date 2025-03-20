// internal/proxy/anthropic/utils.go
package anthropic

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/temp/ghcsd/internal/copilot"
)

// logRequest logs details about an HTTP request
func (h *Handler) logRequest(prefix string, r *http.Request) {
	if !h.debug {
		return
	}
	h.logWithPrefix(prefix, fmt.Sprintf("Method: %s", r.Method))
	h.logWithPrefix(prefix, fmt.Sprintf("URL: %s", r.URL.String()))
	h.logWithPrefix(prefix, "Headers:")
	for name, values := range r.Header {
		for _, value := range values {
			h.logWithPrefix(prefix, fmt.Sprintf("  %s: %s", name, value))
		}
	}
}

// logWithPrefix logs a message with a given prefix
func (h *Handler) logWithPrefix(prefix, message string) {
	if !h.debug {
		return
	}
	// Mask bearer tokens in the message
	maskedMessage := message
	if strings.Contains(message, "Bearer ") {
		parts := strings.Split(message, "Bearer ")
		if len(parts) > 1 {
			tokenPart := parts[1]
			if len(tokenPart) > 20 {
				maskedToken := tokenPart[:5] + "..." + tokenPart[len(tokenPart)-5:]
				maskedMessage = parts[0] + "Bearer " + maskedToken
			}
		}
	}
	log.Printf("[%s] %s\n", prefix, maskedMessage)
}

// sendError sends an error response
func (h *Handler) sendError(w http.ResponseWriter, message string, status int) {
	if h.debug {
		h.logWithPrefix("Error", fmt.Sprintf("%d: %s", status, message))
	}
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"message": message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

// estimateTokenCount provides a rough token count estimation
// In the Python implementation, this uses litellm's token_counter
func (h *Handler) estimateTokenCount(messages []copilot.Message) int {
	// Simple approximation: ~4 characters per token on average
	charCount := 0
	for _, msg := range messages {
		// Count characters in role
		charCount += len(msg.Role)
		// Count characters in content
		if content, ok := msg.Content.(string); ok {
			charCount += len(content)
		} else if complexContent := msg.GetComplexContent(); complexContent != nil {
			for _, item := range complexContent {
				charCount += len(item.Text)
			}
		} else if stringContent := msg.GetStringContent(); stringContent != "" {
			charCount += len(stringContent)
		}
		// Add overhead for message formatting (approx. 10 tokens per message)
		charCount += 40
	}
	// Convert character count to token count (rough approximation)
	tokenCount := charCount / 4
	// Ensure minimum sensible token count
	if tokenCount <= 0 {
		tokenCount = 1
	}
	return tokenCount
}

// logRequestBeautifully logs requests in a format similar to the Python implementation
func (h *Handler) logRequestBeautifully(method, path, claudeModel, openaiModel string, numMessages, numTools int, statusCode int) {
	if !h.debug {
		return
	}
	// Format the Claude model name nicely
	claudeDisplay := colors.Cyan + claudeModel + colors.Reset
	// Extract endpoint name
	endpoint := path
	if strings.Contains(endpoint, "?") {
		endpoint = strings.Split(endpoint, "?")[0]
	}
	// Extract just the OpenAI model name without provider prefix
	openaiDisplay := openaiModel
	if strings.Contains(openaiDisplay, "/") {
		parts := strings.Split(openaiDisplay, "/")
		openaiDisplay = parts[len(parts)-1]
	}
	openaiDisplay = colors.Green + openaiDisplay + colors.Reset
	// Format tools and messages
	toolsStr := colors.Magenta + fmt.Sprintf("%d tools", numTools) + colors.Reset
	messagesStr := colors.Blue + fmt.Sprintf("%d messages", numMessages) + colors.Reset
	// Format status code
	statusStr := ""
	if statusCode == http.StatusOK {
		statusStr = colors.Green + "✓ " + fmt.Sprintf("%d", statusCode) + " OK" + colors.Reset
	} else {
		statusStr = colors.Red + "✗ " + fmt.Sprintf("%d", statusCode) + colors.Reset
	}
	// Put it all together in a clear, beautiful format
	logLine := colors.Bold + method + " " + endpoint + colors.Reset + " " + statusStr
	modelLine := claudeDisplay + " → " + openaiDisplay + " " + toolsStr + " " + messagesStr
	// Print to console
	fmt.Println(logLine)
	fmt.Println(modelLine)
}
