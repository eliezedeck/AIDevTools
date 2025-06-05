package main

import (
	"context"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
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
func (sm *SessionManager) CreateSession(sessionID string, ctx context.Context) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	session := &Session{
		ID:        sessionID,
		Processes: []string{},
		Context:   ctx,
	}
	
	sm.sessions[sessionID] = session
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
	}
}

// RemoveSession removes a session and returns its process IDs
func (sm *SessionManager) RemoveSession(sessionID string) []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if session, exists := sm.sessions[sessionID]; exists {
		processes := session.Processes
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

// ExtractSessionFromRequest extracts session ID from an MCP request context
func ExtractSessionFromRequest(request mcp.CallToolRequest) string {
	// Check if we're in SSE mode
	if globalSSEServer == nil {
		return "" // stdio mode, no session
	}
	
	// In SSE mode, the session ID should be available through the server's context
	// For now, return empty string as we need to investigate how mark3labs/mcp-go
	// passes session context to tool handlers
	// TODO: Extract session ID from SSE context when available
	
	return ""
}