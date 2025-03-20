package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// prismMockServer manages the Prism mock server instance
type prismMockServer struct {
	serverURL string
	apiSpec   string
}

// setupPrismMock starts a Prism mock server with the given OpenAPI spec
func setupPrismMock(t *testing.T) *prismMockServer {
	// Create a temporary directory for the OpenAPI spec
	tempDir, err := os.MkdirTemp("", "prism-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create OpenAPI spec file for Anthropic API
	specPath := filepath.Join(tempDir, "anthropic-api.yaml")
	specContent := `
openapi: 3.1.0
info:
  title: Anthropic API
  version: 1.0.0
servers:
  - url: https://api.anthropic.com
paths:
  /v1/messages:
    post:
      summary: Create a message
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  type:
                    type: string
                  role:
                    type: string
                  content:
                    type: array
            text/event-stream:
              schema:
                type: string
  /v1/messages/count_tokens:
    post:
      summary: Count tokens in a message
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
                properties:
                  input_tokens:
                    type: integer
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("Failed to write OpenAPI spec: %v", err)
	}

	// Start Prism server (mock implementation for testing)
	// Note: In a real environment, you'd need Prism installed
	// Here we're simulating its behavior with an HTTP test server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate the auth header - but in our mock, we'll accept any token format
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		// We'll accept any token format for testing
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Authorization header must start with Bearer", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/v1/messages" {
			// Read request body
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}

			// Check for stream parameter
			stream, ok := req["stream"].(bool)

			if stream && ok {
				// Handle streaming response
				w.Header().Set("Content-Type", "text/event-stream")

				// Simulate Anthropic streaming responses
				events := []string{
					`{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","content":[],"model":"claude-3-opus-20240229","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
					`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"This"}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" is"}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" a"}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" mock"}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" stream"}}`,
					`{"type":"content_block_stop","index":0}`,
					`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"usage":{"output_tokens":5}}}`,
					`{"type":"message_stop"}`,
				}

				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "Streaming not supported", http.StatusInternalServerError)
					return
				}

				for _, event := range events {
					fmt.Fprintf(w, "data: %s\n\n", event)
					flusher.Flush()
					time.Sleep(5 * time.Millisecond) // Simulate delay
				}

				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
			} else {
				// Handle non-streaming response
				w.Header().Set("Content-Type", "application/json")

				// Anthropic format response
				response := map[string]interface{}{
					"id":   "msg_mock",
					"type": "message",
					"role": "assistant",
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "This is a mock response",
						},
					},
					"model":         "claude-3-opus-20240229",
					"stop_reason":   "end_turn",
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens":  10,
						"output_tokens": 5,
					},
				}

				json.NewEncoder(w).Encode(response)
			}
		} else if r.URL.Path == "/v1/messages/count_tokens" {
			w.Header().Set("Content-Type", "application/json")

			tokenResponse := map[string]interface{}{
				"input_tokens": 15, // Mock token count
			}

			json.NewEncoder(w).Encode(tokenResponse)
		} else if strings.HasPrefix(r.URL.Path, "/anthropic/health") {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]interface{}{
				"status":  "ok",
				"message": "Anthropic API proxy is healthy",
			}
			json.NewEncoder(w).Encode(response)
		} else {
			http.NotFound(w, r)
		}
	}))

	return &prismMockServer{
		serverURL: mockServer.URL,
		apiSpec:   specPath,
	}
}

// setupTestServer creates a test server with a client using the Prism mock
func setupTestServer(t *testing.T) (*httptest.Server, *Handler) {
	// Set up Prism mock server
	prismMock := setupPrismMock(t)
	// Create a custom HTTP client that redirects all requests to our mock server
	mockClient := &http.Client{
		Transport: &mockTransport{
			mockURL: prismMock.serverURL,
		},
	}
	// Create real client but point it to our mock server
	client, err := NewClient("mock-token", prismMock.serverURL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	// Replace the internal HTTP client with our custom mock client
	client.SetHTTPClient(mockClient)
	client.SetDebug(true)
	handler := &Handler{
		client:          client,
		defaultModel:    "gpt-4o",
		debug:           true,
		useOpenAIModels: true,
		bigModel:        "gpt-4o",
		smallModel:      "gpt-4o-mini",
	}
	server := httptest.NewServer(handler)
	return server, handler
}

// mockTransport redirects requests to the mock server
type mockTransport struct {
	mockURL string
}

// RoundTrip implements the http.RoundTripper interface
func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a new URL using the mock server URL as the base
	mockReq := req.Clone(req.Context())
	mockURL, err := url.Parse(t.mockURL + req.URL.Path)
	if err != nil {
		return nil, err
	}
	mockReq.URL = mockURL
	mockReq.Host = mockURL.Host

	// Ensure the Authorization header is present
	if mockReq.Header.Get("Authorization") == "" {
		mockReq.Header.Set("Authorization", "Bearer mock-token")
	}

	// Send the request to our mock server
	return http.DefaultTransport.RoundTrip(mockReq)
}

// TestScenario represents a test case configuration
type TestScenario struct {
	name       string
	model      string
	maxTokens  int
	messages   []Message
	tools      []Tool
	toolChoice *ToolChoice
	system     string
	stream     bool
}

// Test scenarios matching the Python implementation
var testScenarios = map[string]TestScenario{
	"simple": {
		name:      "Simple text response",
		model:     "gpt-4o",
		maxTokens: 300,
		messages: []Message{
			{
				Role:    "user",
				Content: "Hello, world! Can you tell me about Paris in 2-3 sentences?",
			},
		},
	},
	"calculator": {
		name:      "Basic tool use",
		model:     "gpt-4o",
		maxTokens: 300,
		messages: []Message{
			{
				Role:    "user",
				Content: "What is 135 + 7.5 divided by 2.5?",
			},
		},
		tools: []Tool{
			{
				Name:        "calculator",
				Description: "Evaluate mathematical expressions",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "The mathematical expression to evaluate",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
		toolChoice: &ToolChoice{Type: "auto"},
	},
	"multi_turn": {
		name:      "Multi-turn conversation",
		model:     "gpt-4o",
		maxTokens: 500,
		messages: []Message{
			{
				Role:    "user",
				Content: "Let's do some math. What is 240 divided by 8?",
			},
			{
				Role:    "assistant",
				Content: "To calculate 240 divided by 8, I'll perform the division:\n\n240 ÷ 8 = 30\n\nSo the result is 30.",
			},
			{
				Role:    "user",
				Content: "Now multiply that by 4 and tell me the result.",
			},
		},
		tools: []Tool{
			{
				Name:        "calculator",
				Description: "Evaluate mathematical expressions",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "The mathematical expression to evaluate",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
		toolChoice: &ToolChoice{Type: "auto"},
	},
	"multi_tool": {
		name:      "Multiple tools",
		model:     "gpt-4o",
		maxTokens: 500,
		system:    "You are a helpful assistant that uses tools when appropriate. Be concise and precise.",
		messages: []Message{
			{
				Role:    "user",
				Content: "I'm planning a trip to New York next week. What's the weather like and what are some interesting places to visit?",
			},
		},
		tools: []Tool{
			{
				Name:        "weather",
				Description: "Get weather information for a location",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city or location to get weather for",
						},
						"units": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"celsius", "fahrenheit"},
							"description": "Temperature units",
						},
					},
					"required": []string{"location"},
				},
			},
			{
				Name:        "search",
				Description: "Search for information on the web",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		toolChoice: &ToolChoice{Type: "auto"},
	},
}

// compareResponses compares the responses from the handler and validates their structure
func compareResponses(t *testing.T, response *http.Response, scenario TestScenario) {
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Expected status code 200, got %d. Response: %s", response.StatusCode, string(body))
	}

	var resp Response
	if err := json.NewDecoder(response.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Basic structure verification
	if resp.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", resp.Role)
	}
	if resp.Type != "message" {
		t.Errorf("Expected type 'message', got '%s'", resp.Type)
	}

	// Verify stop reason is valid
	validStopReasons := []string{"end_turn", "max_tokens", "stop_sequence", "tool_use", ""}
	validStop := false
	for _, reason := range validStopReasons {
		if resp.StopReason == reason {
			validStop = true
			break
		}
	}
	if !validStop {
		t.Errorf("Invalid stop reason: %s", resp.StopReason)
	}

	// Check content
	if len(resp.Content) == 0 {
		t.Error("Response content is empty")
	}

	// Additional checks for tool usage if tools were provided
	if len(scenario.tools) > 0 {
		hasToolUse := false
		for _, content := range resp.Content {
			if content.Type == "tool_use" {
				hasToolUse = true
				if content.ToolCalls == nil {
					t.Error("Tool use content has no tool calls")
				}
				break
			}
		}
		t.Logf("Tool use detected: %v", hasToolUse)
	}
}

// TestBasicRequests tests non-streaming requests
func TestBasicRequests(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	client := server.Client()

	for name, scenario := range testScenarios {
		t.Run(name, func(t *testing.T) {
			// Create request body
			reqBody := Request{
				Model:     scenario.model,
				MaxTokens: scenario.maxTokens,
				Messages:  scenario.messages,
				Tools:     scenario.tools,
				System:    scenario.system,
			}
			if scenario.toolChoice != nil {
				reqBody.ToolChoice = scenario.toolChoice
			}

			reqBytes, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			// Create request
			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				server.URL+"/v1/messages",
				bytes.NewReader(reqBytes),
			)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Header.Set("Content-Type", "application/json")
			startTime := time.Now()

			// Send request
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			t.Logf("Response time: %.2f seconds", time.Since(startTime).Seconds())

			// Compare responses
			compareResponses(t, resp, scenario)
		})
	}
}

// StreamStats tracks statistics about a streaming response
type StreamStats struct {
	EventTypes       map[string]bool
	EventCounts      map[string]int
	FirstEventTime   time.Time
	LastEventTime    time.Time
	TotalChunks      int
	TextContent      string
	HasToolUse       bool
	HasError         bool
	ErrorMessage     string
	ContentByBlockID map[int]string
}

// NewStreamStats creates a new StreamStats instance
func NewStreamStats() *StreamStats {
	return &StreamStats{
		EventTypes:       make(map[string]bool),
		EventCounts:      make(map[string]int),
		ContentByBlockID: make(map[int]string),
	}
}

// AddEvent processes and records information about an event
func (s *StreamStats) AddEvent(event map[string]interface{}) {
	now := time.Now()
	if s.FirstEventTime.IsZero() {
		s.FirstEventTime = now
	}
	s.LastEventTime = now
	s.TotalChunks++

	// Record event type
	if eventType, ok := event["type"].(string); ok {
		s.EventTypes[eventType] = true
		s.EventCounts[eventType]++

		switch eventType {
		case "content_block_start":
			if block, ok := event["content_block"].(map[string]interface{}); ok {
				if blockType, ok := block["type"].(string); ok && blockType == "tool_use" {
					s.HasToolUse = true
				}
			}
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
					if text, ok := delta["text"].(string); ok {
						s.TextContent += text
						if index, ok := event["index"].(float64); ok {
							s.ContentByBlockID[int(index)] += text
						}
					}
				}
			}
		}
	}
}

// GetDuration returns the duration of the stream
func (s *StreamStats) GetDuration() time.Duration {
	if s.FirstEventTime.IsZero() {
		return 0
	}
	return s.LastEventTime.Sub(s.FirstEventTime)
}

// Required event types for streaming responses
var requiredEventTypes = []string{
	"message_start",
	"content_block_start",
	"content_block_delta",
	"content_block_stop",
	"message_delta",
	"message_stop",
}

// compareStreamStats compares two StreamStats instances
func compareStreamStats(t *testing.T, stats *StreamStats) bool {
	t.Logf("Total chunks: %d", stats.TotalChunks)
	t.Logf("Event types: %v", stats.EventTypes)
	t.Logf("Event counts: %v", stats.EventCounts)
	t.Logf("Duration: %.2f seconds", stats.GetDuration().Seconds())
	t.Logf("Has tool use: %v", stats.HasToolUse)

	// Check for required events
	missingEvents := []string{}
	for _, required := range requiredEventTypes {
		if !stats.EventTypes[required] {
			missingEvents = append(missingEvents, required)
		}
	}

	if len(missingEvents) > 0 {
		t.Logf("Missing required event types: %v", missingEvents)
	} else {
		t.Log("All required event types present")
	}

	// Preview content
	if stats.TextContent != "" {
		lines := strings.Split(strings.TrimSpace(stats.TextContent), "\n")
		previewLines := 5
		if len(lines) > previewLines {
			lines = lines[:previewLines]
		}
		t.Logf("Text preview:\n%s", strings.Join(lines, "\n"))
	} else {
		t.Log("No text content extracted")
	}

	if stats.HasError {
		t.Logf("Error: %s", stats.ErrorMessage)
		return false
	}

	// Success if we have some content or tool use and no errors
	return (len(stats.TextContent) > 0 || stats.HasToolUse) && !stats.HasError
}

// processStream reads and processes a streaming response
func processStream(t *testing.T, resp *http.Response) (*StreamStats, error) {
	stats := NewStreamStats()
	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return stats, fmt.Errorf("error reading stream: %v", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimSpace(data)

			if data == "[DONE]" {
				break
			}

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Logf("Error unmarshaling event: %v", err)
				continue
			}

			stats.AddEvent(event)
		}
	}

	return stats, nil
}

// streamingTestScenarios defines test scenarios for streaming
var streamingTestScenarios = map[string]TestScenario{
	"simple_stream": {
		name:      "Simple streaming response",
		model:     "gpt-4o",
		maxTokens: 100,
		stream:    true,
		messages: []Message{
			{
				Role:    "user",
				Content: "Count from 1 to 5, with one number per line.",
			},
		},
	},
	"calculator_stream": {
		name:      "Calculator with streaming",
		model:     "gpt-4o",
		maxTokens: 300,
		stream:    true,
		messages: []Message{
			{
				Role:    "user",
				Content: "What is 135 + 17.5 divided by 2.5?",
			},
		},
		tools: []Tool{
			{
				Name:        "calculator",
				Description: "Evaluate mathematical expressions",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "The mathematical expression to evaluate",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
		toolChoice: &ToolChoice{Type: "auto"},
	},
}

// TestStreamingRequests tests streaming requests
func TestStreamingRequests(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	client := server.Client()

	for name, scenario := range streamingTestScenarios {
		t.Run(name, func(t *testing.T) {
			reqBody := Request{
				Model:     scenario.model,
				MaxTokens: scenario.maxTokens,
				Messages:  scenario.messages,
				Tools:     scenario.tools,
				Stream:    true,
			}
			if scenario.toolChoice != nil {
				reqBody.ToolChoice = scenario.toolChoice
			}

			reqBytes, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				server.URL+"/v1/messages",
				bytes.NewReader(reqBytes),
			)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Header.Set("Content-Type", "application/json")
			startTime := time.Now()

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status code 200, got %d. Response: %s", resp.StatusCode, string(body))
			}

			stats, err := processStream(t, resp)
			if err != nil {
				t.Fatalf("Failed to process stream: %v", err)
			}

			t.Logf("Response time: %.2f seconds", time.Since(startTime).Seconds())

			if !compareStreamStats(t, stats) {
				t.Error("Stream validation failed")
			}
		})
	}
}

// TestHealthCheck tests the health check endpoint
func TestHealthCheck(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	client := server.Client()

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		server.URL+"/anthropic/health",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	var health struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", health.Status)
	}
	if health.Message != "Anthropic API proxy is healthy" {
		t.Errorf("Unexpected health message: %s", health.Message)
	}
}

// TestTokenCount tests the token counting endpoint
func TestTokenCount(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	client := server.Client()

	testCases := []struct {
		name     string
		request  TokenCountRequest
		wantErr  bool
		checkLen bool // whether to check if input_tokens > 0
	}{
		{
			name: "simple text",
			request: TokenCountRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{
						Role:    "user",
						Content: "Hello, how are you?",
					},
				},
			},
			checkLen: true,
		},
		{
			name: "with system message",
			request: TokenCountRequest{
				Model:  "gpt-4o",
				System: "You are a helpful assistant.",
				Messages: []Message{
					{
						Role:    "user",
						Content: "What is 2+2?",
					},
				},
			},
			checkLen: true,
		},
		{
			name: "with tools",
			request: TokenCountRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{
						Role:    "user",
						Content: "Calculate 123 + 456",
					},
				},
				Tools: []Tool{
					{
						Name:        "calculator",
						Description: "Calculate mathematical expressions",
						InputSchema: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"expression": map[string]interface{}{
									"type":        "string",
									"description": "The mathematical expression to evaluate",
								},
							},
							"required": []string{"expression"},
						},
					},
				},
			},
			checkLen: true,
		},
		{
			name: "invalid model",
			request: TokenCountRequest{
				Model: "invalid-model",
				Messages: []Message{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqBytes, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				server.URL+"/anthropic/v1/messages/count_tokens",
				bytes.NewReader(reqBytes),
			)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			if tc.wantErr {
				if resp.StatusCode == http.StatusOK {
					t.Error("Expected error response but got 200 OK")
				}
				return
			}

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status code 200, got %d. Response: %s", resp.StatusCode, string(body))
			}

			var tokenCount TokenCountResponse
			if err := json.NewDecoder(resp.Body).Decode(&tokenCount); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tc.checkLen && tokenCount.InputTokens == 0 {
				t.Error("Expected non-zero input tokens")
			}

			t.Logf("Token count for %s: %d", tc.name, tokenCount.InputTokens)
		})
	}
}
