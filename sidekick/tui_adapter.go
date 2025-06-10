package main

import (
	"context"
	"fmt"
	"time"
)

// TUIManager manages the TUI application lifecycle
type TUIManager struct {
	app    *TUIApp
	ctx    context.Context
	cancel context.CancelFunc
}

// NewTUIManager creates a new TUI manager
func NewTUIManager() *TUIManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &TUIManager{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the TUI application
func (tm *TUIManager) Start() error {
	// TUI starting - no logging allowed in TUI mode
	
	// Create the TUI application
	tm.app = NewTUIApp()
	
	// Start the TUI application (blocks until stopped)
	return tm.app.Run()
}

// Stop stops the TUI application
func (tm *TUIManager) Stop() {
	if tm.app != nil {
		tm.app.Stop()
	}
	tm.cancel()
}

// IsTUIMode returns true if we should run in TUI mode
func IsTUIMode() bool {
	// TUI is only available in SSE mode
	return globalSSEServer != nil
}

// StartTUIIfEnabled starts the TUI if conditions are met
func StartTUIIfEnabled() *TUIManager {
	if !IsTUIMode() {
		return nil
	}
	
	tuiManager := NewTUIManager()
	
	// Start TUI in a separate goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic recovered, but can't log in TUI mode
			}
		}()
		
		// Small delay to ensure SSE server is fully started
		time.Sleep(100 * time.Millisecond)
		
		if err := tuiManager.Start(); err != nil {
			// TUI error occurred, but can't log in TUI mode
		}
	}()
	
	return tuiManager
}

// Helper functions for TUI integration

// GetAllProcessesForTUI returns all processes for TUI display
func GetAllProcessesForTUI() []*ProcessTracker {
	return registry.getAllProcesses()
}

// GetProcessForTUI returns a specific process for TUI display
func GetProcessForTUI(processID string) (*ProcessTracker, bool) {
	return registry.getProcess(processID)
}

// GetSessionsForTUI returns all active sessions for TUI display
func GetSessionsForTUI() map[string]*Session {
	if sessionManager == nil {
		return make(map[string]*Session)
	}
	return sessionManager.GetAllSessions()
}

// GetNotificationHistoryForTUI returns notification history for TUI display
func GetNotificationHistoryForTUI() []NotificationEntry {
	return notificationManager.GetHistory()
}

// ClearNotificationHistoryForTUI clears notification history from TUI
func ClearNotificationHistoryForTUI() {
	notificationManager.ClearHistory()
}

// SetNotificationSoundEnabledForTUI sets notification sound state from TUI
func SetNotificationSoundEnabledForTUI(enabled bool) {
	notificationManager.SetSoundEnabled(enabled)
}

// IsNotificationSoundEnabledForTUI returns notification sound state for TUI
func IsNotificationSoundEnabledForTUI() bool {
	return notificationManager.IsSoundEnabled()
}

// SendProcessInputForTUI sends input to a process from TUI
func SendProcessInputForTUI(processID, input string) error {
	tracker, exists := registry.getProcess(processID)
	if !exists {
		return nil
	}
	
	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()
	
	if tracker.Status != StatusRunning {
		return nil
	}
	
	if tracker.StdinWriter == nil {
		return nil
	}
	
	// Send input with newline
	finalInput := input + "\n"
	_, err := tracker.StdinWriter.Write([]byte(finalInput))
	if err != nil {
		return fmt.Errorf("failed to write to process stdin: %w", err)
	}
	return nil
}

// GetProcessOutputForTUI gets process output for TUI display
func GetProcessOutputForTUI(processID string) (stdout, stderr string, err error) {
	tracker, exists := registry.getProcess(processID)
	if !exists {
		return "", "", nil
	}
	
	tracker.Mutex.RLock()
	defer tracker.Mutex.RUnlock()
	
	if tracker.CombineOutput {
		stdout = tracker.StdoutBuffer.GetContent()
		stderr = ""
	} else {
		stdout = tracker.StdoutBuffer.GetContent()
		if tracker.StderrBuffer != nil {
			stderr = tracker.StderrBuffer.GetContent()
		}
	}
	
	return stdout, stderr, nil
}