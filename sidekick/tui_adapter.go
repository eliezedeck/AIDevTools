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

// Start starts the TUI application with proper error handling and cleanup
func (tm *TUIManager) Start() error {
	// Set up panic recovery for TUI crashes
	defer func() {
		if r := recover(); r != nil {
			// TUI crashed - perform emergency cleanup
			tm.handleTUICrash(r)
		}
	}()

	// TUI starting - no logging allowed in TUI mode
	// Create the TUI application
	tm.app = NewTUIApp()

	// Start the TUI application (blocks until stopped)
	return tm.app.Run()
}

// handleTUICrash handles TUI crashes with proper cleanup and auto-recovery
func (tm *TUIManager) handleTUICrash(panicValue interface{}) {
	// Mark TUI as inactive and crashed immediately
	setTUIActive(false)
	setTUICrashed(true)

	// Force terminal reset to restore normal terminal state
	ForceTerminalReset()

	// Log the crash with emergency logging (bypasses TUI state checks)
	panicMsg := fmt.Sprintf("TUI crashed with panic: %v", panicValue)
	EmergencyLog("TUI", "TUI application crashed unexpectedly", panicMsg)

	// Try to stop the TUI application gracefully if it exists
	if tm.app != nil {
		// Use a timeout to prevent hanging during cleanup
		done := make(chan struct{})
		go func() {
			defer close(done)
			tm.app.Stop()
		}()

		// Wait for cleanup with timeout
		select {
		case <-done:
			EmergencyLog("TUI", "TUI cleanup completed successfully")
		case <-time.After(2 * time.Second):
			EmergencyLog("TUI", "TUI cleanup timed out - forcing exit")
		}
	}

	// Cancel context to signal shutdown
	tm.cancel()

	// Give user a moment to see the error message, then attempt recovery
	time.Sleep(1 * time.Second)

	// Attempt auto-recovery
	EmergencyLog("TUI", "Attempting TUI auto-recovery...")
	go attemptTUIRecovery()
}

// Stop stops the TUI application
func (tm *TUIManager) Stop() {
	// Cancel context first to signal shutdown
	tm.cancel()

	// Stop the TUI application if it exists
	if tm.app != nil {
		tm.app.Stop()
	}
}

// IsTUIMode returns true if we should run in TUI mode
func IsTUIMode() bool {
	// TUI is only available in SSE mode
	return globalSSEServer != nil
}

// StartTUIIfEnabled starts the TUI if conditions are met with proper error handling
func StartTUIIfEnabled() *TUIManager {
	if !IsTUIMode() {
		return nil
	}

	tuiManager := NewTUIManager()

	// Start TUI in a separate goroutine with comprehensive error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic in TUI startup - perform emergency cleanup
				setTUIActive(false)
				setTUICrashed(true)
				ForceTerminalReset()
				EmergencyLog("TUI", "TUI startup failed with panic", fmt.Sprintf("%v", r))

				// Cancel the TUI manager context
				tuiManager.cancel()

				// Attempt auto-recovery
				EmergencyLog("TUI", "Attempting TUI auto-recovery after startup failure...")
				go attemptTUIRecovery()
			}
		}()

		// Small delay to ensure SSE server is fully started
		time.Sleep(100 * time.Millisecond)

		if err := tuiManager.Start(); err != nil {
			// TUI error occurred - ensure proper cleanup
			setTUIActive(false)
			setTUICrashed(true)
			ForceTerminalReset()
			EmergencyLog("TUI", "TUI failed to start", err.Error())

			// Attempt auto-recovery
			EmergencyLog("TUI", "Attempting TUI auto-recovery after start failure...")
			go attemptTUIRecovery()
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
