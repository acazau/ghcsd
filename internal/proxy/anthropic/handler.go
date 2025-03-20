// internal/proxy/anthropic/handler.go
package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/temp/ghcsd/internal/config"
)

// Handler handles requests in Anthropic API format
type Handler struct {
	client       *Client
	defaultModel string
	debug        bool
	// Model mapping configuration
	useOpenAIModels bool
	bigModel        string
	smallModel      string
}

// NewHandler creates a new handler for Anthropic API requests
func NewHandler(apiKey string, defaultModel string, debug bool) (*Handler, error) {
	// Validate default model using the config validation function
	_, valid := config.ValidateModel(defaultModel)
	if !valid {
		return nil, fmt.Errorf("invalid default model: %s", defaultModel)
	}

	// Create a new Anthropic client
	client, err := NewClient(apiKey, "")
	if err != nil {
		return nil, err
	}

	client.SetDebug(debug)

	// Get model mapping configuration from environment variables
	useOpenAIModels := true // Default to true like in Python version

	// Get big and small model mappings from environment
	bigModel := os.Getenv("BIG_MODEL")
	if bigModel == "" {
		bigModel = "gpt-4o" // Default value
	}

	smallModel := os.Getenv("SMALL_MODEL")
	if smallModel == "" {
		smallModel = "gpt-4o-mini" // Default value
	}

	return &Handler{
		client:          client,
		defaultModel:    defaultModel,
		debug:           debug,
		useOpenAIModels: useOpenAIModels,
		bigModel:        bigModel,
		smallModel:      smallModel,
	}, nil
}

// ServeHTTP handles Anthropic API requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.debug {
		h.logRequest("Anthropic Request", r)
	}

	basePath := "/anthropic"
	path := strings.TrimPrefix(r.URL.Path, basePath)

	// Handle health check endpoint
	if r.Method == http.MethodGet && path == "/health" {
		h.handleHealth(w, r)
		return
	}

	// Handle token counting endpoint
	if r.Method == http.MethodPost && path == "/v1/messages/count_tokens" {
		h.handleTokenCount(w, r)
		return
	}

	// Only support messages endpoint for now
	if r.Method != http.MethodPost || path != "/v1/messages" {
		h.sendError(w, "Method not allowed or invalid path", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if h.debug {
		h.logWithPrefix("Anthropic Request", string(body))
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Map model based on configuration
	modelToUse, err := h.mapModel(req.Model)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Invalid model requested: %s", req.Model), http.StatusBadRequest)
		return
	}

	// Update the model in the request
	req.Model = modelToUse

	// Log request beautifully
	numTools := len(req.Tools)
	numMessages := len(req.Messages)
	h.logRequestBeautifully(r.Method, r.URL.Path, req.Model, modelToUse, numMessages, numTools, http.StatusOK)

	// Get the real model ID
	realModelID, valid := config.ValidateModel(modelToUse)
	if !valid {
		h.sendError(w, fmt.Sprintf("Invalid model requested: %s (mapped from %s)", modelToUse, req.Model), http.StatusBadRequest)
		return
	}

	// We have the model ID now, we'll use it in the request
	req.Model = realModelID

	var responseBody io.ReadCloser
	startTime := time.Now()

	if req.Stream {
		// Handle streaming request
		responseBody, err = h.client.CompleteStream(r.Context(), req)
		if err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Streaming completion failed: %v", err))
			h.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Handle streaming response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Set up stream processor
		streamProcessor := &StreamProcessor{
			debug:   h.debug,
			handler: h,
			model:   modelToUse, // Use the original mapped model in the response
			flusher: w.(http.Flusher),
			writer:  w,
		}

		if err := streamProcessor.ProcessStream(responseBody); err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Stream processing failed: %v", err))
			return
		}
	} else {
		// Handle non-streaming request
		resp, err := h.client.Complete(r.Context(), req)
		if err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Completion failed: %v", err))
			h.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Simply pass through the response since we're already using the Anthropic format
		respBytes, err := json.Marshal(resp)
		if err != nil {
			h.sendError(w, "Failed to marshal response", http.StatusInternalServerError)
			return
		}

		responseBody = io.NopCloser(bytes.NewReader(respBytes))

		// Set response headers
		w.Header().Set("Content-Type", "application/json")

		// Write the response
		io.Copy(w, responseBody)
		responseBody.Close()
	}

	if h.debug {
		duration := time.Since(startTime)
		h.logWithPrefix("Timing", fmt.Sprintf("Request processed in %v", duration))
	}
}

// mapModel performs model mapping based on configured rules
func (h *Handler) mapModel(model string) (string, error) {
	if model == "" {
		return h.defaultModel, nil
	}

	// Store the original model
	originalModel := model

	// Handle models with provider prefixes
	model = strings.TrimPrefix(model, "anthropic/")

	// If using OpenAI models, map accordingly
	if h.useOpenAIModels {
		// Swap Haiku with small model
		if strings.Contains(strings.ToLower(model), "haiku") {
			if h.debug {
				h.logWithPrefix("Model Mapping", fmt.Sprintf("Mapping %s → %s", originalModel, h.smallModel))
			}
			return h.smallModel, nil
		}

		// Swap any Sonnet model with big model
		if strings.Contains(strings.ToLower(model), "sonnet") {
			if h.debug {
				h.logWithPrefix("Model Mapping", fmt.Sprintf("Mapping %s → %s", originalModel, h.bigModel))
			}
			return h.bigModel, nil
		}

		// No specific mapping needed
		return model, nil
	} else {
		// If not using OpenAI models, ensure anthropic/ prefix
		if h.debug && originalModel != model {
			h.logWithPrefix("Model Mapping", fmt.Sprintf("Keeping original model: %s", model))
		}
		return model, nil
	}
}

// handleHealth returns a health status response
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":  "ok",
		"message": "Anthropic API proxy is healthy",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleTokenCount implements token counting endpoint
func (h *Handler) handleTokenCount(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if h.debug {
		h.logWithPrefix("Token Count Request", string(body))
	}

	var req TokenCountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Map model based on configuration
	modelToUse, err := h.mapModel(req.Model)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Invalid model requested: %s", req.Model), http.StatusBadRequest)
		return
	}

	// Update the model in the request
	req.Model = modelToUse

	// Get the real model ID
	realModelID, valid := config.ValidateModel(modelToUse)
	if !valid {
		h.sendError(w, fmt.Sprintf("Invalid model requested: %s (mapped from %s)", modelToUse, req.Model), http.StatusBadRequest)
		return
	}

	// Use the real model ID in the request
	req.Model = realModelID

	// Call the token counting API
	tokenCount, err := h.client.CountTokens(r.Context(), req)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Failed to count tokens: %v", err), http.StatusInternalServerError)
		return
	}

	// Respond with token count
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Log the response if in debug mode
	if h.debug {
		respBytes, _ := json.Marshal(tokenCount)
		h.logWithPrefix("Token Count Response", string(respBytes))
	}

	json.NewEncoder(w).Encode(tokenCount)
}

// logRequest logs the full details of an incoming request
func (h *Handler) logRequest(prefix string, r *http.Request) {
	fmt.Printf("[%s] %s %s\n", prefix, r.Method, r.URL.Path)
	for name, values := range r.Header {
		for _, value := range values {
			fmt.Printf("[%s] %s: %s\n", prefix, name, value)
		}
	}
}

// logWithPrefix logs a message with a prefix for better readability
func (h *Handler) logWithPrefix(prefix string, message string) {
	fmt.Printf("[%s] %s\n", prefix, message)
}

// logRequestBeautifully logs a request in an easily readable format
func (h *Handler) logRequestBeautifully(method string, path string, originalModel string, mappedModel string, messageCount int, toolCount int, statusCode int) {
	if !h.debug {
		return
	}

	var modelInfo string
	if originalModel != mappedModel {
		modelInfo = fmt.Sprintf("%s → %s", originalModel, mappedModel)
	} else {
		modelInfo = originalModel
	}

	toolInfo := ""
	if toolCount > 0 {
		toolInfo = fmt.Sprintf(" with %d tools", toolCount)
	}

	fmt.Printf("[Request] %s %s | Model: %s | %d messages%s | Status: %d\n",
		method, path, modelInfo, messageCount, toolInfo, statusCode)
}

// sendError sends a formatted error response
func (h *Handler) sendError(w http.ResponseWriter, message string, statusCode int) {
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"message": message,
		},
	}

	if h.debug {
		h.logWithPrefix("Error", fmt.Sprintf("%d: %s", statusCode, message))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
