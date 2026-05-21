package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitConfig(t *testing.T) {
	// Set XDG_CONFIG_HOME to a temporary directory to avoid writing to the user's actual config.
	tmpDir, err := os.MkdirTemp("", "teams-tui-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldXdg := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", oldXdg)
	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Case 1: Config does not exist yet.
	InitConfig()

	appDir, err := GetAppDir()
	if err != nil {
		t.Fatalf("GetAppDir failed: %v", err)
	}
	configPath := filepath.Join(appDir, "config.json")

	// Verify config file was created.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse generated config: %v", err)
	}

	// Verify defaults.
	if cfg.ClientID == nil || *cfg.ClientID != defaultClientID {
		t.Errorf("expected client ID %q, got %v", defaultClientID, cfg.ClientID)
	}
	if cfg.NotificationMode == nil || *cfg.NotificationMode != NotificationNone {
		t.Errorf("expected notification mode %v, got %v", NotificationNone, cfg.NotificationMode)
	}
	if cfg.NotificationShowPreview == nil || *cfg.NotificationShowPreview != false {
		t.Errorf("expected notification show preview false, got %v", cfg.NotificationShowPreview)
	}
	if cfg.NotificationPreviewLen == nil || *cfg.NotificationPreviewLen != 50 {
		t.Errorf("expected notification preview len 50, got %v", cfg.NotificationPreviewLen)
	}
	if cfg.MessageLimit == nil || *cfg.MessageLimit != 50 {
		t.Errorf("expected message limit 50, got %v", cfg.MessageLimit)
	}
	if cfg.SearchContextLimit == nil || *cfg.SearchContextLimit != 3 {
		t.Errorf("expected search context limit 3, got %v", cfg.SearchContextLimit)
	}

	// Case 2: Config exists but is missing some options (e.g. partial).
	// We'll write a custom config with only ClientID and MessageLimit set, and others missing/nil.
	customClientID := "custom-id-123"
	customMsgLimit := 100
	partialCfg := Config{
		ClientID:     &customClientID,
		MessageLimit: &customMsgLimit,
	}
	partialData, err := json.Marshal(partialCfg)
	if err != nil {
		t.Fatalf("failed to marshal partial config: %v", err)
	}
	if err := os.WriteFile(configPath, partialData, 0o600); err != nil {
		t.Fatalf("failed to write partial config: %v", err)
	}

	// Run InitConfig to fill in the missing ones.
	InitConfig()

	// Re-read and check.
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var updatedCfg Config
	if err := json.Unmarshal(data, &updatedCfg); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	// Custom values must be preserved.
	if updatedCfg.ClientID == nil || *updatedCfg.ClientID != customClientID {
		t.Errorf("expected preserved client ID %q, got %v", customClientID, updatedCfg.ClientID)
	}
	if updatedCfg.MessageLimit == nil || *updatedCfg.MessageLimit != customMsgLimit {
		t.Errorf("expected preserved message limit %d, got %v", customMsgLimit, updatedCfg.MessageLimit)
	}

	// Missing values must be populated.
	if updatedCfg.NotificationMode == nil || *updatedCfg.NotificationMode != NotificationNone {
		t.Errorf("expected default notification mode, got %v", updatedCfg.NotificationMode)
	}
	if updatedCfg.SearchContextLimit == nil || *updatedCfg.SearchContextLimit != 3 {
		t.Errorf("expected default search context limit 3, got %v", updatedCfg.SearchContextLimit)
	}
}

func TestResolveMessageLimit(t *testing.T) {
	// Set XDG_CONFIG_HOME to a temporary directory.
	tmpDir, err := os.MkdirTemp("", "teams-tui-config-test-limit")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldXdg := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", oldXdg)
	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Write custom config with message_limit = 52.
	appDir, err := GetAppDir()
	if err != nil {
		t.Fatalf("GetAppDir failed: %v", err)
	}
	configPath := filepath.Join(appDir, "config.json")

	limit := 52
	cfg := Config{
		MessageLimit: &limit,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Resolve the message limit. It must be capped at 50.
	resolved := ResolveMessageLimit()
	if resolved != 50 {
		t.Errorf("expected message limit to be capped at 50, got %d", resolved)
	}
}
