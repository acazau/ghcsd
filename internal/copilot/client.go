// internal/copilot/client.go
package copilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client handles communication with the Copilot API
type Client struct {
	client    *http.Client
	token     string
	model     string
	sessionID string
	machineID string
	baseURL   string
	debug     bool
}

// NewClient creates a new Copilot client instance
func NewClient(token string, model string, copilotAPIURL string) (*Client, error) {
	return &Client{
		client:    &http.Client{},
		token:     token,
		model:     model,
		sessionID: generateSessionID(),
		machineID: generateMachineID(),
		baseURL:   "https://api.githubcopilot.com",
		debug:     false,
	}, nil
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	return uuid.New().String() + fmt.Sprint(time.Now().UnixNano()/int64(time.Millisecond))
}

// generateMachineID creates a consistent machine identifier
func generateMachineID() string {
	return uuid.New().String()
}

func (c *Client) logWithPrefix(prefix, message string) {
	if !c.debug {
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

func (c *Client) logRequest(prefix string, r *http.Request) {
	if !c.debug {
		return
	}
	c.logWithPrefix(prefix, fmt.Sprintf("Method: %s", r.Method))
	c.logWithPrefix(prefix, fmt.Sprintf("URL: %s", r.URL.String()))
	c.logWithPrefix(prefix, "Headers:")
	for name, values := range r.Header {
		for _, value := range values {
			c.logWithPrefix(prefix, fmt.Sprintf("  %s: %s", name, value))
		}
	}
}

// CompleteStream sends a completion request to the Copilot API and returns a stream
func (c *Client) CompleteStream(ctx context.Context, messages []Message) (io.ReadCloser, error) {
	req := NewCompletionRequest(c.model)
	req.Stream = true
	req.Messages = messages

	return c.sendRequest(ctx, req)
}

// Complete sends a completion request to the Copilot API and returns a response
func (c *Client) Complete(ctx context.Context, messages []Message) (*CompletionResponse, error) {
	req := NewCompletionRequest(c.model)
	req.Messages = messages

	body, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	respBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var response CompletionResponse
	if err := json.Unmarshal(respBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// sendRequest handles the common logic for sending requests to the Copilot API
func (c *Client) sendRequest(ctx context.Context, req CompletionRequest) (io.ReadCloser, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.debug {
		c.logWithPrefix("Copilot Request", string(body))
	}

	apiURL := fmt.Sprintf("%s/chat/completions", c.baseURL)
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		apiURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	token := strings.TrimSpace(c.token)
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Editor-Version", "vscode/0.1.0")
	httpReq.Header.Set("copilot-integration-id", "vscode-chat")
	httpReq.Header.Set("VScode-SessionId", c.sessionID)
	httpReq.Header.Set("VScode-MachineId", c.machineID)
	httpReq.Header.Set("X-Request-Id", uuid.New().String())

	if c.debug {
		c.logRequest("Copilot Request", httpReq)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Read error response
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	if req.Stream {
		return c.handleStream(resp.Body), nil
	}

	// For non-streaming responses, log the response body
	if c.debug {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			c.logWithPrefix("Copilot Response", string(respBody))
			// Create new reader with the same content
			return io.NopCloser(bytes.NewReader(respBody)), nil
		}
		// If we failed to read the body for logging, return the original
		return resp.Body, nil
	}

	return resp.Body, nil
}

// handleStream processes the streaming response from Copilot
func (c *Client) handleStream(body io.ReadCloser) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()
	streamReader := &streamReader{
		reader: bufio.NewReader(body),
		debug:  c.debug,
		client: c,
	}

	go func() {
		defer body.Close()
		defer pipeWriter.Close()

		for {
			line, err := streamReader.reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Error reading stream: %v\n", err)
				}
				return
			}

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			if bytes.HasPrefix(line, []byte("data: ")) {
				line = bytes.TrimPrefix(line, []byte("data: "))
			}

			if bytes.Equal(line, []byte("[DONE]")) {
				finalMsg := CompletionResponse{
					Choices: []Choice{
						{
							Message: struct {
								Content string `json:"content"`
								Role    string `json:"role"`
							}{
								Content: "",
								Role:    "assistant",
							},
							FinishReason: "stop",
						},
					},
				}
				if data, err := json.Marshal(finalMsg); err == nil {
					if c.debug {
						c.logWithPrefix("Copilot Response", string(data))
					}
					fmt.Fprintf(pipeWriter, "data: %s\n\n", data)
				}
				return
			}

			// Log the raw response line
			if c.debug {
				c.logWithPrefix("Copilot Response", string(line))
			}

			var response CompletionResponse
			if err := json.Unmarshal(line, &response); err != nil {
				fmt.Printf("Error unmarshalling stream: %v, line: %s\n", err, string(line))
				continue
			}

			if len(response.Choices) > 0 {
				if data, err := json.Marshal(response); err == nil {
					fmt.Fprintf(pipeWriter, "data: %s\n\n", data)
				}
			}
		}
	}()

	return pipeReader
}

type streamReader struct {
	reader *bufio.Reader
	debug  bool
	client *Client
}

func (sr *streamReader) Read(p []byte) (int, error) {
	return sr.reader.Read(p)
}

func (sr *streamReader) Close() error {
	return nil
}

// SetDebug enables or disables debug logging
func (c *Client) SetDebug(debug bool) {
	c.debug = debug
}

// GetModel returns the model configured for this client
func (c *Client) GetModel() string {
	return c.model
}

// GetToken returns the token configured for this client
func (c *Client) GetToken() string {
	return c.token
}
