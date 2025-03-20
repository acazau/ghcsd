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
	"github.com/temp/ghcsd/internal/copilot"
)

// Handler handles requests in Anthropic API format and converts them to Copilot format
type Handler struct {
	client       *copilot.Client
	defaultModel string
	debug        bool
	// Model mapping configuration
	useOpenAIModels bool
	bigModel        string
	smallModel      string
}

// NewHandler creates a new handler for Anthropic API requests
func NewHandler(token string, defaultModel string, debug bool) (*Handler, error) {
	// Validate default model using the config validation function
	realModelID, valid := config.ValidateModel(defaultModel)
	if !valid {
		return nil, fmt.Errorf("invalid default model: %s", defaultModel)
	}

	client, err := copilot.NewClient(token, realModelID, "")
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

	// Normalize the path by trimming prefix
	path := strings.TrimPrefix(r.URL.Path, "/anthropic")

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

	// Use the mapped model
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

	// Create a new client instance with the selected model
	client, err := copilot.NewClient(h.client.GetToken(), realModelID, "")
	if err != nil {
		h.sendError(w, "Failed to create client", http.StatusInternalServerError)
		return
	}
	client.SetDebug(h.debug)

	// Convert Anthropic messages to Copilot format
	copilotMessages, err := h.convertAnthropicToCopilotMessages(req)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Failed to convert messages: %v", err), http.StatusBadRequest)
		return
	}

	var responseBody io.ReadCloser
	startTime := time.Now()

	if req.Stream {
		responseBody, err = client.CompleteStream(r.Context(), copilotMessages)
		if err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Streaming completion failed: %v", err))
			h.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Handle streaming response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		streamProcessor := &StreamProcessor{
			debug:   h.debug,
			handler: h,
			model:   req.Model, // Use original model in response
			flusher: w.(http.Flusher),
			writer:  w,
		}

		if err := streamProcessor.ProcessStream(responseBody); err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Stream processing failed: %v", err))
			return
		}
	} else {
		// Handle non-streaming response
		resp, err := client.Complete(r.Context(), copilotMessages)
		if err != nil {
			h.logWithPrefix("Error", fmt.Sprintf("Completion failed: %v", err))
			h.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert Copilot response to Anthropic format
		anthropicResp := h.convertCopilotToAnthropicResponse(resp, req.Model) // Use original model in response

		respBytes, err := json.Marshal(anthropicResp)
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

// mapModel performs model mapping similar to the Python implementation
func (h *Handler) mapModel(model string) (string, error) {
	if model == "" {
		return h.defaultModel, nil
	}

	// Store the original model
	originalModel := model

	// Handle models with provider prefixes
	if strings.HasPrefix(model, "anthropic/") {
		model = model[len("anthropic/"):]
	}

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

// handleTokenCount implements token counting endpoint similar to the Python version
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

	// Convert the request to a format similar to the message request
	anthropicReq := Request{
		Model:     modelToUse,
		Messages:  req.Messages,
		System:    req.System,
		Tools:     req.Tools,
		MaxTokens: 100, // Arbitrary value, not used for counting
	}

	// Convert Anthropic messages to Copilot format
	copilotMessages, err := h.convertAnthropicToCopilotMessages(anthropicReq)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Failed to convert messages: %v", err), http.StatusBadRequest)
		return
	}

	// Calculate approximate token count based on the messages
	inputTokens := h.estimateTokenCount(copilotMessages)

	// Create token count response
	response := TokenCountResponse{
		InputTokens: inputTokens,
	}

	// Respond with token count
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Log the response if in debug mode
	if h.debug {
		respBytes, _ := json.Marshal(response)
		h.logWithPrefix("Token Count Response", string(respBytes))
	}

	json.NewEncoder(w).Encode(response)
}
