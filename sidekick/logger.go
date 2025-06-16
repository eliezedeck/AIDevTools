package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelWarn
	LogLevelError
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     LogLevel  `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"` // Optional additional details
}

// Logger manages application logs
type Logger struct {
	mu            sync.RWMutex
	entries       []LogEntry
	maxEntries    int
	consoleOutput bool // Whether to output to console (disabled during TUI mode)
}

// Global logger instance
var logger = &Logger{
	entries:       make([]LogEntry, 0),
	maxEntries:    1000,
	consoleOutput: true, // Default to console output
}

// SetConsoleOutput enables or disables console output
func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enabled
}

// Log adds a new log entry
func (l *Logger) Log(level LogLevel, source, message string, details ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Message:   message,
	}

	if len(details) > 0 {
		entry.Details = details[0]
	}

	// Add to entries
	l.entries = append(l.entries, entry)

	// Trim if exceeds max entries
	if len(l.entries) > l.maxEntries {
		l.entries = l.entries[len(l.entries)-l.maxEntries:]
	}

	// Output to console if enabled and not in TUI mode
	if l.consoleOutput && !isTUIActiveCheck() {
		timestamp := entry.Timestamp.Format("15:04:05")
		levelStr := entry.Level.String()

		// Format: [HH:MM:SS] LEVEL [Source] Message
		output := fmt.Sprintf("[%s] %s [%s] %s", timestamp, levelStr, source, message)
		if entry.Details != "" {
			output += fmt.Sprintf(" - %s", entry.Details)
		}

		// In non-TUI mode, print to console
		fmt.Println(output)
	}
}

// Info logs an info level message
func (l *Logger) Info(source, message string, details ...string) {
	l.Log(LogLevelInfo, source, message, details...)
}

// Warn logs a warning level message
func (l *Logger) Warn(source, message string, details ...string) {
	l.Log(LogLevelWarn, source, message, details...)
}

// Error logs an error level message
func (l *Logger) Error(source, message string, details ...string) {
	l.Log(LogLevelError, source, message, details...)
}

// GetEntries returns a copy of all log entries
func (l *Logger) GetEntries() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Return a copy to prevent external modification
	entries := make([]LogEntry, len(l.entries))
	copy(entries, l.entries)
	return entries
}

// GetEntriesByLevel returns entries filtered by log level
func (l *Logger) GetEntriesByLevel(level LogLevel) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var filtered []LogEntry
	for _, entry := range l.entries {
		if entry.Level == level {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// Clear removes all log entries
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make([]LogEntry, 0)
}

// GetRecentEntries returns the most recent n entries
func (l *Logger) GetRecentEntries(n int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n > len(l.entries) {
		n = len(l.entries)
	}

	start := len(l.entries) - n
	entries := make([]LogEntry, n)
	copy(entries, l.entries[start:])
	return entries
}

// Helper functions for global logger

// LogInfo logs an info message to the global logger
func LogInfo(source, message string, details ...string) {
	logger.Info(source, message, details...)
}

// LogWarn logs a warning message to the global logger
func LogWarn(source, message string, details ...string) {
	logger.Warn(source, message, details...)
}

// LogError logs an error message to the global logger
func LogError(source, message string, details ...string) {
	logger.Error(source, message, details...)
}

// SetConsoleLogging enables or disables console output for the global logger
func SetConsoleLogging(enabled bool) {
	logger.SetConsoleOutput(enabled)
}

// GetLogEntries returns all log entries from the global logger
func GetLogEntries() []LogEntry {
	return logger.GetEntries()
}

// ClearLogs clears all logs from the global logger
func ClearLogs() {
	logger.Clear()
}

// EmergencyLog outputs critical messages directly to stderr, bypassing all TUI state checks
// This is used for TUI crashes, panics, and other critical errors that must be visible
func EmergencyLog(source, message string, details ...string) {
	timestamp := time.Now().Format("15:04:05")
	output := fmt.Sprintf("[%s] EMERGENCY [%s] %s", timestamp, source, message)
	if len(details) > 0 && details[0] != "" {
		output += fmt.Sprintf(" - %s", details[0])
	}

	// Always output to stderr regardless of TUI state
	fmt.Fprintf(os.Stderr, "%s\n", output)

	// Also add to log entries for TUI display (if TUI recovers)
	logger.Log(LogLevelError, source, fmt.Sprintf("EMERGENCY: %s", message), details...)
}

// ForceTerminalReset attempts to reset the terminal to a normal state
// This is used when TUI crashes to restore terminal functionality
func ForceTerminalReset() {
	// ANSI escape sequences to reset terminal state:
	// \033[?1049l - Exit alternate screen buffer
	// \033[0m     - Reset all attributes (colors, bold, etc.)
	// \033[2J     - Clear entire screen
	// \033[H      - Move cursor to home position (1,1)
	// \033[?25h   - Show cursor
	// \033[?1000l - Disable mouse reporting
	resetSequence := "\033[?1049l\033[0m\033[2J\033[H\033[?25h\033[?1000l"
	fmt.Fprint(os.Stderr, resetSequence)
}
