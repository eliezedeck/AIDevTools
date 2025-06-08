package main

import (
	"context"
	"sync"

	"github.com/mark3labs/mcp-go/server"
)

// SessionManager manages session-to-process mapping for SSE connections
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// Session represents an SSE client session
type Session struct {
	ID        string
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
		Processes: []string{},
		Context:   ctx,
		Cancel:    cancel,
	}
	
	sm.sessions[sessionID] = session
	logIfNotTUI("🔗 [SSE] New session created: %s", sessionID)
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
		logIfNotTUI("🔧 [SSE] Process %s added to session %s (total: %d)", 
			processID, sessionID, len(session.Processes))
	} else {
		// Create session if it doesn't exist (first process for this session)
		sm.mu.Unlock()
		session := sm.CreateSession(sessionID)
		sm.mu.Lock()
		session.Processes = append(session.Processes, processID)
		logIfNotTUI("🔗 [SSE] New session %s created with first process %s", sessionID, processID)
	}
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

// ExtractSessionFromContext extracts session ID from the context
func ExtractSessionFromContext(ctx context.Context) string {
	// Check if we're in SSE mode
	if globalSSEServer == nil {
		return "" // stdio mode, no session
	}
	
	// Extract session from context using mark3labs/mcp-go method
	session := server.ClientSessionFromContext(ctx)
	if session != nil {
		sessionID := session.SessionID()
		return sessionID
	}
	
	return ""
}