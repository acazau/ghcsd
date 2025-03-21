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

	"github.com/joho/godotenv"
)

func init() {
	// Try various possible locations for the .env file
	paths := []string{
		"../../../../.env", // from test file location
		"../../../.env",    // one level up
		"../../.env",       // two levels up
		"../.env",          // three levels up
		".env",             // current directory
	}

	loaded := false
	for _, path := range paths {
		if err := godotenv.Load(path); err == nil {
			loaded = true
			break
		}
	}

	if !loaded {
		fmt.Println("Warning: Could not load .env file from any location")
	}
}

// getMockServerURL returns the URL of the mock server from environment variable
func getMockServerURL() (string, error) {
	mockURL := os.Getenv("MOCK_SERVER_URL")
	if mockURL == "" {
		return "", fmt.Errorf("MOCK_SERVER_URL environment variable not set in .env file")
	}
	return mockURL, nil
}

// setupTestServer creates a test server with a client using the Prism mock
func setupTestServer(t *testing.T) (*httptest.Server, *Handler) {
	mockURL, err := getMockServerURL()
	if err != nil {
		t.Fatal(err)
	}

	// Create a custom HTTP client that redirects all requests to our mock server
	mockClient := &http.Client{
		Transport: &mockTransport{
			mockURL: mockURL,
		},
	}

	// Create real client but point it to our mock server
	client, err := NewClient("mock-token", mockURL)
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

	// Create a client with timeouts
	client := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       5 * time.Second,
			DisableKeepAlives:     true,
		},
		Timeout: 10 * time.Second,
	}

	// Implement retry logic
	maxRetries := 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		resp, err := client.Do(mockReq)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after %d retries: %v", maxRetries, lastErr)
}

// TestScenario represents a test case configuration
type TestScenario struct {
	Name       string      `json:"name"`
	Model      string      `json:"model"`
	MaxTokens  int         `json:"maxTokens"`
	Messages   []Message   `json:"messages"`
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice *ToolChoice `json:"toolChoice,omitempty"`
	System     string      `json:"system,omitempty"`
	Stream     bool        `json:"stream,omitempty"`
	WantErr    bool        `json:"wantErr,omitempty"`
	CheckLen   bool        `json:"checkLen,omitempty"`
}

// loadTestScenarios loads test scenarios from a JSON file
func loadTestScenarios(t *testing.T, filename string) map[string]TestScenario {
	path := filepath.Join("fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read test scenarios file: %v", err)
	}

	var scenarios map[string]TestScenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatalf("Failed to unmarshal test scenarios: %v", err)
	}

	return scenarios
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
	if len(scenario.Tools) > 0 {
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

	scenarios := loadTestScenarios(t, "test_scenarios.json")
	client := server.Client()

	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			reqBody := Request{
				Model:     scenario.Model,
				MaxTokens: scenario.MaxTokens,
				Messages:  scenario.Messages,
				Tools:     scenario.Tools,
				System:    scenario.System,
			}
			if scenario.ToolChoice != nil {
				reqBody.ToolChoice = scenario.ToolChoice
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

// TestStreamingRequests tests streaming requests
func TestStreamingRequests(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	scenarios := loadTestScenarios(t, "streaming_scenarios.json")
	client := server.Client()

	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			reqBody := Request{
				Model:     scenario.Model,
				MaxTokens: scenario.MaxTokens,
				Messages:  scenario.Messages,
				Tools:     scenario.Tools,
				Stream:    true,
			}
			if scenario.ToolChoice != nil {
				reqBody.ToolChoice = scenario.ToolChoice
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

// TokenCountTestScenario represents a test case configuration for token counting
type TokenCountTestScenario struct {
	Name     string    `json:"name"`
	Model    string    `json:"model"`
	System   string    `json:"system,omitempty"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	WantErr  bool      `json:"wantErr"`
	CheckLen bool      `json:"checkLen"`
}

// TestTokenCount tests the token counting endpoint
func TestTokenCount(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	scenarios := loadTestScenarios(t, "token_count_scenarios.json")
	client := server.Client()

	for name, scenario := range scenarios {
		tc := TokenCountTestScenario{
			Name:     scenario.Name,
			Model:    scenario.Model,
			System:   scenario.System,
			Messages: scenario.Messages,
			Tools:    scenario.Tools,
			WantErr:  scenario.WantErr,
			CheckLen: scenario.CheckLen,
		}

		t.Run(name, func(t *testing.T) {
			reqBody := TokenCountRequest{
				Model:    tc.Model,
				System:   tc.System,
				Messages: tc.Messages,
				Tools:    tc.Tools,
			}

			reqBytes, err := json.Marshal(reqBody)
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

			if tc.WantErr {
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

			if tc.CheckLen && tokenCount.InputTokens == 0 {
				t.Error("Expected non-zero input tokens")
			}

			t.Logf("Token count for %s: %d", tc.Name, tokenCount.InputTokens)
		})
	}
}
