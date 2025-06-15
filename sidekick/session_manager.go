package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/server"
)

// SessionManager manages session-to-process mapping for SSE connections
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// SessionStatus represents the state of a session
type SessionStatus string

const (
	SessionConnected    SessionStatus = "connected"
	SessionDisconnected SessionStatus = "disconnected"
)

// Session represents an SSE client session
type Session struct {
	ID        string
	Status    SessionStatus
	Processes []string // Process IDs owned by this session
	Context   context.Context
	Cancel    context.CancelFunc // Cancel function for the session context
}

// Global session manager instance
var sessionManager = &SessionManager{
	sessions: make(map[string]*Session),
}

// GetSessionFromContext extracts session ID from the request context
func GetSessionFromContext(ctx context.Context) string {
	// In SSE mode, the session ID is provided by the SSE server
	// We'll need to extract it from the context
	if globalSSEServer != nil {
		// The SSE server should provide session ID in the context
		// This will be implemented based on how mark3labs/mcp-go handles it
		if sessionID, ok := ctx.Value("sessionID").(string); ok {
			return sessionID
		}
	}
	return ""
}

// CreateSession creates a new session
func (sm *SessionManager) CreateSession(sessionID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session already exists
	if _, exists := sm.sessions[sessionID]; exists {
		return sm.sessions[sessionID]
	}

	// Create a long-lived context for this session
	ctx, cancel := context.WithCancel(context.Background())

	session := &Session{
		ID:        sessionID,
		Status:    SessionConnected,
		Processes: []string{},
		Context:   ctx,
		Cancel:    cancel,
	}

	sm.sessions[sessionID] = session
	LogInfo("Session", "New session created", fmt.Sprintf("SessionID: %s", sessionID))
	return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	return session, exists
}

// AddProcessToSession associates a process with a session
func (sm *SessionManager) AddProcessToSession(sessionID, processID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.Processes = append(session.Processes, processID)
		LogInfo("Session", "Process added to session",
			fmt.Sprintf("ProcessID: %s, SessionID: %s, Total: %d", processID, sessionID, len(session.Processes)))
	} else {
		// Create session if it doesn't exist (first process for this session)
		sm.mu.Unlock()
		session := sm.CreateSession(sessionID)
		sm.mu.Lock()
		session.Processes = append(session.Processes, processID)
		LogInfo("Session", "New session created with first process",
			fmt.Sprintf("SessionID: %s, ProcessID: %s", sessionID, processID))
	}
}

// MarkSessionDisconnected marks a session as disconnected but keeps it in memory
func (sm *SessionManager) MarkSessionDisconnected(sessionID string) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.Status = SessionDisconnected
		processes := session.Processes
		// Cancel the session context
		if session.Cancel != nil {
			session.Cancel()
		}
		LogInfo("Session", "Session marked as disconnected", fmt.Sprintf("SessionID: %s", sessionID))
		return processes
	}

	return []string{}
}

// RemoveSession removes a session and returns its process IDs
func (sm *SessionManager) RemoveSession(sessionID string) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		processes := session.Processes
		// Cancel the session context
		if session.Cancel != nil {
			session.Cancel()
		}
		delete(sm.sessions, sessionID)
		return processes
	}

	return []string{}
}

// GetProcessesBySession returns all process IDs for a session
func (sm *SessionManager) GetProcessesBySession(sessionID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if session, exists := sm.sessions[sessionID]; exists {
		// Return a copy to prevent external modification
		processes := make([]string, len(session.Processes))
		copy(processes, session.Processes)
		return processes
	}

	return []string{}
}

// GetAllSessions returns a copy of all active sessions
func (sm *SessionManager) GetAllSessions() map[string]*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Create a copy to prevent external modification
	sessions := make(map[string]*Session)
	for id, session := range sm.sessions {
		sessions[id] = session
	}

	return sessions
}

// ExtractSessionFromContext extracts session ID from the context and ensures session exists
func ExtractSessionFromContext(ctx context.Context) string {
	// Check if we're in SSE mode
	if globalSSEServer == nil {
		return "" // stdio mode, no session
	}

	// Extract session from context using mark3labs/mcp-go method
	session := server.ClientSessionFromContext(ctx)
	if session != nil {
		sessionID := session.SessionID()

		// Ensure the session exists in our SessionManager
		// This is important because sessions are created by the MCP library
		// but we need to track them in our SessionManager for proper lifecycle management
		sessionManager.EnsureSessionExists(sessionID)

		return sessionID
	}

	return ""
}

// IsSessionActive checks if a session is still active and connected
func (sm *SessionManager) IsSessionActive(sessionID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if session, exists := sm.sessions[sessionID]; exists {
		return session.Status == SessionConnected
	}

	return false
}

// EnsureSessionExists creates a session if it doesn't exist, or returns the existing one
func (sm *SessionManager) EnsureSessionExists(sessionID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session already exists
	if session, exists := sm.sessions[sessionID]; exists {
		return session
	}

	// Create a new session
	ctx, cancel := context.WithCancel(context.Background())

	session := &Session{
		ID:        sessionID,
		Status:    SessionConnected,
		Processes: []string{},
		Context:   ctx,
		Cancel:    cancel,
	}

	sm.sessions[sessionID] = session
	LogInfo("Session", "Session auto-created from context", fmt.Sprintf("SessionID: %s", sessionID))
	return session
}
