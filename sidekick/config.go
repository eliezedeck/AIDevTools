package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SidekickConfig holds persistent configuration
type SidekickConfig struct {
	CursorKeybindingsWatcher CursorKeybindingsWatcherConfig `json:"cursor_keybindings_watcher"`
	Discord                  DiscordConfig                   `json:"discord"`
}

// CursorKeybindingsWatcherConfig holds keybindings watcher preferences
type CursorKeybindingsWatcherConfig struct {
	Enabled       bool   `json:"enabled"`
	LastModified  string `json:"last_modified,omitempty"`
	RemovalsCount int    `json:"removals_count"`
}

// DiscordConfig holds Discord webhook preferences
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url,omitempty"`
}

var configMu sync.Mutex

// configDir returns ~/.sidekick, creating it if needed
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	dir := filepath.Join(home, ".sidekick")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return dir, nil
}

// configPath returns ~/.sidekick/config.json
func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig reads config from disk; returns default config if file missing
func LoadConfig() (*SidekickConfig, error) {
	configMu.Lock()
	defer configMu.Unlock()
	return loadConfigLocked()
}

// loadConfigLocked reads config without acquiring the lock (caller must hold configMu)
func loadConfigLocked() (*SidekickConfig, error) {
	path, err := configPath()
	if err != nil {
		return &SidekickConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SidekickConfig{}, nil
		}
		return &SidekickConfig{}, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg SidekickConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &SidekickConfig{}, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes config to disk atomically
func SaveConfig(cfg *SidekickConfig) error {
	configMu.Lock()
	defer configMu.Unlock()
	return saveConfigLocked(cfg)
}

// saveConfigLocked writes config without acquiring the lock (caller must hold configMu)
func saveConfigLocked(cfg *SidekickConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: temp file + rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up on failure
		return fmt.Errorf("failed to rename config: %w", err)
	}
	return nil
}

// UpdateConfig performs an atomic read-modify-write on the config file.
// The provided function is called with the current config under lock,
// and the modified config is saved back atomically.
func UpdateConfig(fn func(cfg *SidekickConfig)) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil {
		return err
	}
	fn(cfg)
	return saveConfigLocked(cfg)
}
