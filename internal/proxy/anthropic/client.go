package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client handles direct communication with the Anthropic API
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	debug      bool
	sessionID  string
}

// NewClient creates a new client for the Anthropic API
func NewClient(apiKey string, baseURL string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	// Use the provided Anthropic API URL or fall back to the default
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	// Ensure the URL doesn't have a trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Client{
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Default timeout of 2 minutes
		},
		apiKey:    apiKey,
		baseURL:   baseURL,
		debug:     false,
		sessionID: generateSessionID(),
	}, nil
}

// SetDebug enables/disables debug mode
func (c *Client) SetDebug(debug bool) {
	c.debug = debug
}

// SetHTTPClient allows setting a custom HTTP client for testing and mocking
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// GetAPIKey returns the API key for the client
func (c *Client) GetAPIKey() string {
	return c.apiKey
}

// Complete makes a non-streaming request to the Anthropic API
func (c *Client) Complete(ctx context.Context, request Request) (*Response, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.debug {
		fmt.Printf("Anthropic Request: %s\n", string(reqBytes))
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages",
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if c.debug {
		fmt.Printf("Anthropic Response time: %.2f seconds\n", time.Since(startTime).Seconds())
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if c.debug {
		respBytes, _ := json.Marshal(response)
		fmt.Printf("Anthropic Response: %s\n", string(respBytes))
	}

	return &response, nil
}

// CompleteStream makes a streaming request to the Anthropic API
func (c *Client) CompleteStream(ctx context.Context, request Request) (io.ReadCloser, error) {
	// Make sure it's a streaming request
	request.Stream = true

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal streaming request: %w", err)
	}

	if c.debug {
		fmt.Printf("Anthropic Streaming Request: %s\n", string(reqBytes))
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages",
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create streaming request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send streaming request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// CountTokens estimates token usage for a request
func (c *Client) CountTokens(ctx context.Context, request TokenCountRequest) (*TokenCountResponse, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token count request: %w", err)
	}

	if c.debug {
		fmt.Printf("Anthropic Token Count Request: %s\n", string(reqBytes))
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages/count_tokens",
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create token count request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send token count request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response TokenCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode token count response: %w", err)
	}

	if c.debug {
		respBytes, _ := json.Marshal(response)
		fmt.Printf("Anthropic Token Count Response: %s\n", string(respBytes))
	}

	return &response, nil
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	return uuid.New().String()
}
