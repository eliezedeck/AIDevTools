package main

import (
	"fmt"
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