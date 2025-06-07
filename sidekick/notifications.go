package main

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// SpeakParams defines the input for the notifications_speak tool 🚀
type SpeakParams struct {
	Text string `json:"text" mcp:"Text to speak (max 50 words)"`
}

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
			_ = exec.Command("afplay", "/System/Library/Sounds/Glass.aiff", "-v", "5").Run()
		}()

		// 🗣️ Speak the text after a short delay
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = exec.Command("say", "-v", "Zoe (Premium)", text).Run()
		}()
	}

	return mcp.NewToolResultText("Notification spoken!"), nil
}
