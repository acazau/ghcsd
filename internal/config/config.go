// config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

var validModels = map[string]string{
	"gpt-4":             "gpt-4",
	"4":                 "gpt-4",
	"gpt-4o":            "gpt-4o",
	"4o":                "gpt-4o",
	"o1-mini":           "o1-mini",
	"o1-preview":        "o1-preview",
	"sonnet":            "claude-3.5-sonnet",
	"claude-3.5-sonnet": "claude-3.5-sonnet",
}

func GetModels() []string {
	models := make([]string, 0, len(validModels))
	for model := range validModels {
		models = append(models, model)
	}
	return models
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

	model := "gpt-4o"
	if _, ok := validModels[model]; !ok {
		return nil, fmt.Errorf("invalid model: %s", model)
	}

	return &Config{
		ServerAddr: ":8080",
		Model:      model,
		ConfigDir:  configDir,
	}, nil
}
