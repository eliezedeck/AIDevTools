package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp" // Used by handleSpeak
)

// Shared HTTP client with timeout for Discord webhook calls
var discordHTTPClient = &http.Client{Timeout: 10 * time.Second}

// NotificationEntry represents a notification in history
type NotificationEntry struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

// NotificationManager manages notification history and settings
type NotificationManager struct {
	mu              sync.RWMutex
	history         []NotificationEntry
	soundEnabled    bool
	maxHistorySize  int
}

// Global notification manager
var notificationManager = &NotificationManager{
	history:        []NotificationEntry{},
	soundEnabled:   true,
	maxHistorySize: 100,
}

// AddToHistory adds a notification to the history
func (nm *NotificationManager) AddToHistory(text string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	entry := NotificationEntry{
		Text:      text,
		Timestamp: time.Now(),
	}
	
	nm.history = append(nm.history, entry)
	
	// Keep only the last maxHistorySize entries
	if len(nm.history) > nm.maxHistorySize {
		nm.history = nm.history[len(nm.history)-nm.maxHistorySize:]
	}
}

// GetHistory returns a copy of the notification history
func (nm *NotificationManager) GetHistory() []NotificationEntry {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	// Return a copy to prevent external modification
	history := make([]NotificationEntry, len(nm.history))
	copy(history, nm.history)
	return history
}

// ClearHistory clears the notification history
func (nm *NotificationManager) ClearHistory() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.history = []NotificationEntry{}
}

// SetSoundEnabled sets whether notification sounds are enabled
func (nm *NotificationManager) SetSoundEnabled(enabled bool) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.soundEnabled = enabled
}

// IsSoundEnabled returns whether notification sounds are enabled
func (nm *NotificationManager) IsSoundEnabled() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.soundEnabled
}

// handleSpeak executes the notifications_speak tool logic 🎤
func handleSpeak(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := request.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'text' argument"), nil
	}

	words := strings.Fields(text)
	if len(words) > 50 {
		return mcp.NewToolResultError("Text must be 50 words or less"), nil
	}

	// Add to notification history
	notificationManager.AddToHistory(text)

	// Only play sound if enabled
	if notificationManager.IsSoundEnabled() {
		// 🔊 Play system sound asynchronously
		go func() {
			if err := exec.Command("afplay", "/System/Library/Sounds/Glass.aiff", "-v", "5").Run(); err != nil {
				// Log error but don't fail the notification - sound is non-critical
				// In a production system, this would go to a proper logger
				// For now, we'll just continue silently as the sound is optional
			}
		}()

		// 🗣️ Speak the text after a short delay
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := exec.Command("say", "-v", "Zoe (Premium)", text).Run(); err != nil {
				// Log error but don't fail the notification - speech is non-critical
				// The notification has already been recorded in history
				// In a production system, this would go to a proper logger
			}
		}()
	}

	// 📨 Send to Discord webhook (async, regardless of soundEnabled)
	go sendDiscordWebhook(text)

	return mcp.NewToolResultText("Notification spoken!"), nil
}

// sendDiscordWebhook sends a notification to the configured Discord webhook
func sendDiscordWebhook(text string) {
	cfg, err := LoadConfig()
	if err != nil || cfg.Discord.WebhookURL == "" {
		return
	}
	sendDiscordMessage(cfg.Discord.WebhookURL, text)
}

// sendDiscordMessage posts a message to a Discord webhook URL
func sendDiscordMessage(webhookURL, text string) error {
	payload := map[string]any{
		"content":          text,
		"allowed_mentions": map[string]any{"parse": []string{}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		LogError("Discord", "Failed to marshal payload", err.Error())
		return err
	}

	resp, err := discordHTTPClient.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		LogError("Discord", "Failed to send webhook", err.Error())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		LogError("Discord", fmt.Sprintf("Webhook returned status %d", resp.StatusCode))
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// SetDiscordWebhookURL validates and saves the Discord webhook URL
func SetDiscordWebhookURL(url string) error {
	if !strings.HasPrefix(url, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(url, "https://discordapp.com/api/webhooks/") {
		return fmt.Errorf("invalid Discord webhook URL: must start with https://discord.com/api/webhooks/")
	}
	if err := UpdateConfig(func(cfg *SidekickConfig) {
		cfg.Discord.WebhookURL = url
	}); err != nil {
		return err
	}
	LogInfo("Discord", "Webhook URL configured")
	return nil
}

// ClearDiscordWebhookURL removes the Discord webhook URL
func ClearDiscordWebhookURL() error {
	if err := UpdateConfig(func(cfg *SidekickConfig) {
		cfg.Discord.WebhookURL = ""
	}); err != nil {
		return err
	}
	LogInfo("Discord", "Webhook URL cleared")
	return nil
}

// IsDiscordWebhookConfigured returns whether a Discord webhook URL is set
func IsDiscordWebhookConfigured() bool {
	cfg, err := LoadConfig()
	if err != nil {
		return false
	}
	return cfg.Discord.WebhookURL != ""
}

// GetDiscordWebhookURLMasked returns the webhook URL with most of it hidden
func GetDiscordWebhookURLMasked() string {
	cfg, err := LoadConfig()
	if err != nil || cfg.Discord.WebhookURL == "" {
		return ""
	}
	if len(cfg.Discord.WebhookURL) > 8 {
		return "..." + cfg.Discord.WebhookURL[len(cfg.Discord.WebhookURL)-8:]
	}
	return "***"
}

// TestDiscordWebhook sends a test message to the configured webhook
func TestDiscordWebhook() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg.Discord.WebhookURL == "" {
		return fmt.Errorf("no Discord webhook URL configured")
	}
	return sendDiscordMessage(cfg.Discord.WebhookURL, "Test notification from Sidekick")
}
