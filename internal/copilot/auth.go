// File: internal/copilot/auth.go
package copilot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deviceCodeURL = "https://github.com/login/device/code"
	clientID      = "Iv1.b507a08c87ecfe98" // GitHub Copilot client ID
)

// AuthManager handles GitHub Copilot authentication
type AuthManager struct {
	client    *http.Client
	configDir string
	debug     bool
}

// NewAuthManager creates a new AuthManager instance
func NewAuthManager(client *http.Client, configDir string, debug bool) *AuthManager {
	return &AuthManager{
		client:    client,
		configDir: configDir,
		debug:     debug,
	}
}

func (a *AuthManager) debugLog(format string, v ...interface{}) {
	if a.debug {
		log.Printf("[Auth Manager] "+format, v...)
	}
}

// GetCopilotToken initiates the full token acquisition flow
func (a *AuthManager) GetCopilotToken() (string, error) {
	a.debugLog("Starting GetCopilotToken operation")

	// Try to load existing auth token
	authToken, err := a.LoadAuthToken()
	if err != nil {
		a.debugLog("No existing auth token found, starting device code flow")
		// If no auth token exists, start device code flow
		deviceCode, err := a.RequestDeviceCode()
		if err != nil {
			return "", fmt.Errorf("failed to request device code: %w", err)
		}

		authToken, err = a.handleDeviceCodeFlow(deviceCode)
		if err != nil {
			return "", err
		}
	} else {
		a.debugLog("Found existing auth token (masked): %s...%s",
			authToken[:5], authToken[len(authToken)-5:])
	}

	// Always exchange the auth token for a Copilot API token, regardless of whether it's new or existing
	a.debugLog("Exchanging auth token for Copilot API token")
	copilotToken, err := a.fetchNewToken(authToken)
	if err != nil {
		a.debugLog("Failed to get Copilot API token: %v", err)
		
		// If the token exchange fails, it might be because the auth token is expired
		// Try to get a new token through the device code flow
		a.debugLog("Auth token may be expired, starting new device code flow")
		deviceCode, err := a.RequestDeviceCode()
		if err != nil {
			return "", fmt.Errorf("failed to request device code after token exchange failure: %w", err)
		}

		authToken, err = a.handleDeviceCodeFlow(deviceCode)
		if err != nil {
			return "", err
		}
		
		// Try again with the new auth token
		return a.fetchNewToken(authToken)
	}

	a.debugLog("Successfully obtained Copilot API token")
	return copilotToken, nil
}

// LoadAuthToken loads the authentication token from the specified configuration directory
func (a *AuthManager) LoadAuthToken() (string, error) {
	tokenPath := filepath.Join(a.configDir, ".copilot-auth-token")
	a.debugLog("Loading auth token from: %s", tokenPath)
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read auth token file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// DeviceCode represents the response from the device code request
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode initiates the device code flow
func (a *AuthManager) RequestDeviceCode() (*DeviceCode, error) {
	a.debugLog("Requesting device code from GitHub...")

	reqBody := bytes.NewBuffer([]byte(fmt.Sprintf(`{"client_id":"%s","scope":"copilot"}`, clientID)))

	req, err := http.NewRequest("POST", deviceCodeURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	a.debugLog("Device code response status: %s", resp.Status)

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("HTTP error: %s", resp.Status)
		}
		return nil, fmt.Errorf("device code error: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var deviceCode DeviceCode
	if err := json.Unmarshal(body, &deviceCode); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	a.debugLog("Successfully received device code with verification URI: %s", deviceCode.VerificationURI)
	return &deviceCode, nil
}

// handleDeviceCodeFlow manages the device code authorization flow
func (a *AuthManager) handleDeviceCodeFlow(deviceCode *DeviceCode) (string, error) {
	fmt.Printf("\nPlease visit: %s\n", deviceCode.VerificationURI)
	fmt.Printf("And enter code: %s\n", deviceCode.UserCode)
	a.debugLog("Waiting for user to authorize the device code")

	authResp, err := a.pollForAuthorization(deviceCode)
	if err != nil {
		return "", err
	}

	// Save the new auth token
	if err := a.SaveAuthToken(authResp.AccessToken); err != nil {
		a.debugLog("Failed to save auth token: %v", err)
		return "", fmt.Errorf("failed to save auth token: %w", err)
	}

	a.debugLog("Successfully saved new auth token")
	return authResp.AccessToken, nil
}

// pollForAuthorization continuously checks for device code authorization
func (a *AuthManager) pollForAuthorization(deviceCode *DeviceCode) (*AuthResponse, error) {
	tokenURL := "https://github.com/login/oauth/access_token"
	startTime := time.Now()

	for {
		reqBody := bytes.NewBuffer([]byte(fmt.Sprintf(`{
			"client_id": "%s",
			"device_code": "%s",
			"grant_type": "urn:ietf:params:oauth:grant-type:device_code"
		}`, clientID, deviceCode.DeviceCode)))

		req, err := http.NewRequest("POST", tokenURL, reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create token request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("token request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			var authResp AuthResponse
			if err := json.Unmarshal(body, &authResp); err != nil {
				return nil, fmt.Errorf("failed to parse auth response: %w", err)
			}

			if authResp.AccessToken != "" {
				return &authResp, nil
			}
		}

		if time.Since(startTime) > time.Duration(deviceCode.ExpiresIn)*time.Second {
			return nil, fmt.Errorf("device code expired")
		}

		a.debugLog("Polling for authorization... waiting %d seconds", deviceCode.Interval)
		time.Sleep(time.Duration(deviceCode.Interval) * time.Second)
	}
}

// fetchNewToken gets a new Copilot API token using the auth token
func (a *AuthManager) fetchNewToken(authToken string) (string, error) {
	a.debugLog("Initiating new token fetch from GitHub API")

	req, err := http.NewRequest("GET", "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", authToken))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/0.1.0")
	req.Header.Set("copilot-integration-id", "vscode-chat")

	a.debugLog("Sending token request to GitHub API")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	a.debugLog("Received response with status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		a.debugLog("Error response from API: %s", string(body))
		return "", fmt.Errorf("failed to get token (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		a.debugLog("Failed to parse API response: %v", err)
		return "", err
	}

	if result.Token == "" {
		a.debugLog("API returned empty token")
		return "", fmt.Errorf("received empty token from API")
	}

	a.debugLog("Successfully parsed token response")
	return result.Token, nil
}

type AuthResponse struct {
	AccessToken string `json:"access_token"`
}

type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// SaveAuthToken saves the authentication token to the config directory
func (a *AuthManager) SaveAuthToken(token string) error {
	tokenPath := filepath.Join(a.configDir, ".copilot-auth-token")
	return os.WriteFile(tokenPath, []byte(token), 0600)
}

// RemoveAuthToken removes the saved authentication token
func (a *AuthManager) RemoveAuthToken() error {
	tokenPath := filepath.Join(a.configDir, ".copilot-auth-token")
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
