package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tidwall/jsonc"
)

// KeybindingsWatcher manages the fsnotify watcher for Cursor keybindings
type KeybindingsWatcher struct {
	filePath string
	mu       sync.Mutex // Protects concurrent processFile calls
	cancel   context.CancelFunc
}

// Global watcher instance (nil when not running)
var (
	keybindingsWatcher   *KeybindingsWatcher
	keybindingsWatcherMu sync.Mutex
)

// cursorKeybindingsFilePath returns the OS-appropriate path to Cursor's keybindings.json
func cursorKeybindingsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "keybindings.json"), nil
	case "linux":
		return filepath.Join(home, ".config", "Cursor", "User", "keybindings.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "Cursor", "User", "keybindings.json"), nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// StartKeybindingsWatcher starts the background watcher goroutine
func StartKeybindingsWatcher() error {
	keybindingsWatcherMu.Lock()
	defer keybindingsWatcherMu.Unlock()

	// Stop existing watcher if running
	if keybindingsWatcher != nil {
		keybindingsWatcher.cancel()
		keybindingsWatcher = nil
	}

	filePath, err := cursorKeybindingsFilePath()
	if err != nil {
		return fmt.Errorf("failed to determine keybindings path: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &KeybindingsWatcher{
		filePath: filePath,
		cancel:   cancel,
	}
	keybindingsWatcher = w

	go w.run(ctx)
	LogInfo("KeybindingsWatcher", "Started watching "+filePath)
	return nil
}

// StopKeybindingsWatcher stops the background watcher goroutine
func StopKeybindingsWatcher() {
	keybindingsWatcherMu.Lock()
	defer keybindingsWatcherMu.Unlock()

	if keybindingsWatcher != nil {
		keybindingsWatcher.cancel()
		keybindingsWatcher = nil
		LogInfo("KeybindingsWatcher", "Stopped")
	}
}

// run is the main event loop for the keybindings watcher
func (w *KeybindingsWatcher) run(ctx context.Context) {
	// Initial check on startup
	w.processFile()

	parentDir := filepath.Dir(w.filePath)
	fileName := filepath.Base(w.filePath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		LogError("KeybindingsWatcher", "Failed to create fsnotify watcher", err.Error())
		return
	}
	defer watcher.Close()

	if err := watcher.Add(parentDir); err != nil {
		LogError("KeybindingsWatcher", "Failed to watch directory", parentDir, err.Error())
		return
	}

	var debounceTimer *time.Timer
	proactiveTicker := time.NewTicker(1 * time.Minute)
	defer proactiveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only react to our target file
			if filepath.Base(event.Name) != fileName {
				continue
			}
			// Handle Write, Create, and Rename events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
					w.processFile()
				})
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			LogError("KeybindingsWatcher", "fsnotify error", err.Error())

		case <-proactiveTicker.C:
			w.processFile()
		}
	}
}

// The correct shift+enter binding that Claude Code needs in Cursor's terminal
var correctShiftEnterBinding = map[string]any{
	"key":     "shift+enter",
	"command": "workbench.action.terminal.sendSequence",
	"args": map[string]any{
		"text": "\x1b\r",
	},
	"when": "terminalFocus",
}

// hasCorrectShiftEnterBinding checks if the exact correct Claude Code binding exists
func hasCorrectShiftEnterBinding(entries []map[string]any) bool {
	for _, entry := range entries {
		key, _ := entry["key"].(string)
		cmd, _ := entry["command"].(string)
		when, _ := entry["when"].(string)
		if !strings.EqualFold(strings.TrimSpace(key), "shift+enter") {
			continue
		}
		if cmd != "workbench.action.terminal.sendSequence" || when != "terminalFocus" {
			continue
		}
		args, ok := entry["args"].(map[string]any)
		if !ok {
			continue
		}
		if args["text"] == "\x1b\r" {
			return true
		}
	}
	return false
}

// processFile ensures the correct shift+enter binding exists in Cursor's keybindings
func (w *KeybindingsWatcher) processFile() {
	w.mu.Lock()
	defer w.mu.Unlock()

	entries, err := readKeybindings(w.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return // File doesn't exist yet — normal, skip silently
		}
		LogError("KeybindingsWatcher", "Failed to read keybindings", err.Error())
		return
	}

	// If the correct binding already exists and there are no other shift+enter entries, nothing to do
	if hasCorrectShiftEnterBinding(entries) {
		_, extraCount := removeShiftEnterBindings(entries)
		if extraCount == 1 { // Only the correct one
			return
		}
	}

	// Remove all shift+enter bindings (correct or incorrect), then add the correct one back
	filtered, removedCount := removeShiftEnterBindings(entries)
	filtered = append(filtered, correctShiftEnterBinding)

	if err := writeKeybindings(w.filePath, filtered); err != nil {
		LogError("KeybindingsWatcher", "Failed to write keybindings", err.Error())
		return
	}

	if removedCount > 0 {
		LogInfo("KeybindingsWatcher",
			fmt.Sprintf("Replaced %d incorrect shift+enter binding(s) with the correct Claude Code binding", removedCount))
	} else {
		LogInfo("KeybindingsWatcher", "Added missing shift+enter binding for Claude Code")
	}

	_ = UpdateConfig(func(cfg *SidekickConfig) {
		cfg.CursorKeybindingsWatcher.LastModified = time.Now().UTC().Format(time.RFC3339)
		cfg.CursorKeybindingsWatcher.RemovalsCount += removedCount
	})
}

// readKeybindings reads and parses the keybindings file (JSONC format)
func readKeybindings(filePath string) ([]map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Convert JSONC (comments + trailing commas) to standard JSON
	cleanJSON := jsonc.ToJSON(data)

	var entries []map[string]any
	if err := json.Unmarshal(cleanJSON, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse keybindings JSON: %w", err)
	}
	return entries, nil
}

// writeKeybindings writes the keybindings file atomically, preserving permissions
func writeKeybindings(filePath string, entries []map[string]any) error {
	// Preserve original file permissions
	mode := os.FileMode(0644)
	if info, err := os.Stat(filePath); err == nil {
		mode = info.Mode()
	}

	data, err := json.MarshalIndent(entries, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal keybindings: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: temp file in same directory + rename
	dir := filepath.Dir(filePath)
	tmpPath := filepath.Join(dir, ".keybindings.json.tmp")
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return fmt.Errorf("failed to write temp keybindings: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename keybindings: %w", err)
	}
	return nil
}

// removeShiftEnterBindings filters out entries where key == "shift+enter" (case-insensitive)
func removeShiftEnterBindings(entries []map[string]any) ([]map[string]any, int) {
	var filtered []map[string]any
	removed := 0
	for _, entry := range entries {
		if key, ok := entry["key"].(string); ok {
			if strings.EqualFold(strings.TrimSpace(key), "shift+enter") {
				removed++
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered, removed
}

// EnableKeybindingsWatcher enables the watcher and saves the preference
func EnableKeybindingsWatcher() error {
	// Start watcher first — only persist config if startup succeeds
	if err := StartKeybindingsWatcher(); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}
	if err := UpdateConfig(func(c *SidekickConfig) {
		c.CursorKeybindingsWatcher.Enabled = true
	}); err != nil {
		StopKeybindingsWatcher() // Rollback: stop watcher if config save fails
		return fmt.Errorf("failed to save config: %w", err)
	}
	LogInfo("KeybindingsWatcher", "Enabled")
	return nil
}

// DisableKeybindingsWatcher disables the watcher and saves the preference
func DisableKeybindingsWatcher() error {
	StopKeybindingsWatcher()
	if err := UpdateConfig(func(c *SidekickConfig) {
		c.CursorKeybindingsWatcher.Enabled = false
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	LogInfo("KeybindingsWatcher", "Disabled")
	return nil
}

// IsKeybindingsWatcherEnabled returns whether the watcher is enabled in config
func IsKeybindingsWatcherEnabled() bool {
	cfg, err := LoadConfig()
	if err != nil {
		return false
	}
	return cfg.CursorKeybindingsWatcher.Enabled
}
