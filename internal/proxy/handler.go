// internal/proxy/handler.go
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/acazau/ghcsd/internal/config"
	"github.com/acazau/ghcsd/internal/copilot"
)

type Handler struct {
	client       *copilot.Client
	defaultModel string
	debug        bool
}

func NewHandler(token string, defaultModel string, debug bool) (*Handler, error) {
	// Validate default model
	models := config.GetModels()
	valid := false
	for _, m := range models {
		if m == defaultModel {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid default model: %s", defaultModel)
	}

	client, err := copilot.NewClient(token, defaultModel, "")
	if err != nil {
		return nil, err
	}
	client.SetDebug(debug)
	return &Handler{
		client:       client,
		defaultModel: defaultModel,
		debug:        debug,
	}, nil
}

type ErrorResponse struct {
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.debug {
		h.logRequest("Client Request", r)
	}

	// Handle health check endpoint
	if r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
		h.handleHealth(w, r)
		return
	}

	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		h.sendError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if h.debug {
		h.logWithPrefix("Client Request", string(body))
	}

	var req copilot.CompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate and use requested model if provided, otherwise use default
	if req.Model != "" {
		models := config.GetModels()
		valid := false
		for _, m := range models {
			if m == req.Model {
				valid = true
				break
			}
		}
		if !valid {
			h.sendError(w, fmt.Sprintf("Invalid model requested: %s", req.Model), http.StatusBadRequest)
			return
		}
	} else {
		req.Model = h.defaultModel
	}

	// Create a new client instance with the selected model
	client, err := copilot.NewClient(h.client.GetToken(), req.Model, "")
	if err != nil {
		h.sendError(w, "Failed to create client", http.StatusInternalServerError)
		return
	}
	client.SetDebug(h.debug)

	var responseBody io.ReadCloser
	if req.Stream {
		responseBody, err = client.CompleteStream(r.Context(), req.Messages)
	} else {
		var resp *copilot.CompletionResponse
		resp, err = client.Complete(r.Context(), req.Messages)
		if err == nil {
			respBytes, err := json.Marshal(resp)
			if err == nil {
				responseBody = io.NopCloser(bytes.NewReader(respBytes))
			}
		}
	}

	if err != nil {
		if h.debug {
			h.logWithPrefix("Error", fmt.Sprintf("Completion failed: %v", err))
		}
		h.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer responseBody.Close()

	// Set appropriate headers for the response
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}

	rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	var buf bytes.Buffer
	reader := io.TeeReader(responseBody, &buf)

	_, err = io.Copy(rw, reader)
	if err != nil {
		if h.debug {
			h.logWithPrefix("Error", fmt.Sprintf("Error copying response: %v", err))
		}
		return
	}

	if h.debug {
		h.logResponse("Client Response", rw, buf.String())
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{
		Status:  "ok",
		Message: "Service is healthy",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) sendError(w http.ResponseWriter, message string, status int) {
	if h.debug {
		h.logWithPrefix("Error", fmt.Sprintf("%d: %s", status, message))
	}

	response := ErrorResponse{
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

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

func (h *Handler) logResponse(prefix string, w *responseWriter, body string) {
	h.logWithPrefix(prefix, fmt.Sprintf("Status: %d %s", w.statusCode, http.StatusText(w.statusCode)))
	h.logWithPrefix(prefix, "Headers:")
	for name, values := range w.Header() {
		for _, value := range values {
			h.logWithPrefix(prefix, fmt.Sprintf("  %s: %s", name, value))
		}
	}
	h.logWithPrefix(prefix, "Body:")
	h.logWithPrefix(prefix, body)
}
