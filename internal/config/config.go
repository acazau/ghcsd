// config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Model represents an AI model with its properties
type Model struct {
	ID       string // User-friendly ID for the model
	RealID   string // Actual ID used in API requests
	Provider string // Provider of the model (OpenAI, Anthropic, Google)
}

// List of supported models
var models = []Model{
	{ID: "gpt-4", RealID: "gpt-4", Provider: "OpenAI"},
	{ID: "4", RealID: "gpt-4", Provider: "OpenAI"},
	{ID: "gpt-4o", RealID: "gpt-4o", Provider: "OpenAI"},
	{ID: "4o", RealID: "gpt-4o", Provider: "OpenAI"},
	{ID: "o1", RealID: "o1", Provider: "OpenAI"},
	{ID: "o3-mini", RealID: "o3-mini", Provider: "OpenAI"},
	{ID: "sonnet", RealID: "claude-3.7-sonnet", Provider: "Anthropic"},
	{ID: "claude-3.5-sonnet", RealID: "claude-3.5-sonnet", Provider: "Anthropic"},
	{ID: "claude-3.7-sonnet", RealID: "claude-3.7-sonnet", Provider: "Anthropic"},
	{ID: "claude-3.7-sonnet-thought", RealID: "claude-3.7-sonnet-thought", Provider: "Anthropic"},
	{ID: "gemini-2.0-flash", RealID: "gemini-2.0-flash-001", Provider: "Google"},
	{ID: "gemini-2.5-pro", RealID: "gemini-2.5-pro-preview-03-25", Provider: "Google"},
	{ID: "gemini-flash", RealID: "gemini-2.0-flash-001", Provider: "Google"},
	{ID: "gemini-pro", RealID: "gemini-2.5-pro-preview-03-25", Provider: "Google"},
}

// modelMap provides quick lookups for model validation and mapping
var modelMap map[string]Model

func init() {
	// Initialize the model map
	modelMap = make(map[string]Model)
	for _, model := range models {
		modelMap[strings.ToLower(model.ID)] = model
	}
}

// GetModelList returns a list of all available model IDs
func GetModelList() []string {
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		modelIDs = append(modelIDs, model.ID)
	}
	return modelIDs
}

// GetModelsByProvider returns models filtered by provider
func GetModelsByProvider(provider string) []Model {
	var result []Model
	for _, model := range models {
		if model.Provider == provider {
			result = append(result, model)
		}
	}
	return result
}

// ValidateModel checks if the provided model name is valid and returns the real model ID
func ValidateModel(modelName string) (string, bool) {
	model, ok := modelMap[strings.ToLower(modelName)]
	if !ok {
		return "", false
	}
	return model.RealID, true
}

// GetModelInfo returns detailed information about a model by its name
func GetModelInfo(modelName string) (Model, bool) {
	model, ok := modelMap[strings.ToLower(modelName)]
	return model, ok
}

type Config struct {
	ServerAddr string
	Model      string
	ConfigDir  string
}

func New() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "ghcsd")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	defaultModel := "gpt-4o"
	realModelID, ok := ValidateModel(defaultModel)
	if !ok {
		return nil, fmt.Errorf("invalid model: %s", defaultModel)
	}

	return &Config{
		ServerAddr: ":8080",
		Model:      realModelID,
		ConfigDir:  configDir,
	}, nil
}
