package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const appDirName = "teams-tui-go"

// defaultClientID is the Microsoft Teams client ID fallback.
const defaultClientID = "d3590ed6-52b3-4102-aeff-aad2292ab01c"

// Config holds persistent application settings.
type Config struct {
	ClientID                *string           `json:"client_id,omitempty"`
	NotificationMode        *NotificationMode `json:"notification_mode,omitempty"`
	NotificationShowPreview *bool             `json:"notification_show_preview,omitempty"`
	NotificationPreviewLen  *int              `json:"notification_preview_len,omitempty"`
}

// GetAppDir returns ~/.config/teams-tui-go/, creating it if necessary.
func GetAppDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config dir: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create config dir: %w", err)
	}
	return dir, nil
}

// GetCacheDir returns ~/.cache/teams-tui-go/, creating it if necessary.
func GetCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine cache dir: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create cache dir: %w", err)
	}
	return dir, nil
}

// LoadConfig reads config.json from the app dir.
// Returns nil if the file does not exist or cannot be parsed.
func LoadConfig() *Config {
	dir, err := GetAppDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// SaveConfig writes config.json (pretty-printed) to the app dir.
func SaveConfig(cfg *Config) error {
	dir, err := GetAppDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
}

// ResolveClientID returns the client ID using the precedence:
//  1. CLIENT_ID environment variable (loads .env first)
//  2. config.json → client_id
//  3. Built-in default
func ResolveClientID() string {
	// Load .env file if present; ignore errors (file may not exist).
	_ = godotenv.Load()

	if id := os.Getenv("CLIENT_ID"); id != "" {
		return id
	}
	cfg := LoadConfig()
	if cfg != nil && cfg.ClientID != nil && *cfg.ClientID != "" {
		return *cfg.ClientID
	}
	return defaultClientID
}
