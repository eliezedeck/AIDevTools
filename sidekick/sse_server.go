package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"strings"
)

// SSEServerConfig holds configuration for the SSE server
type SSEServerConfig struct {
	Host string
	Port string
}

// sseHandler wraps the SSE server to track connections
type sseHandler struct {
	sseServer *server.SSEServer
}

// ServeHTTP implements http.Handler
func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is an SSE connection request
	if strings.HasSuffix(r.URL.Path, "/sse") {
		// Extract session ID from the SSE response
		// We'll need to wrap the response writer to capture the session ID
		wrapped := &responseWrapper{
			ResponseWriter: w,
			request:       r,
		}
		
		// Call the original SSE handler
		h.sseServer.ServeHTTP(wrapped, r)
		
		// If we captured a session ID, track the connection
		if wrapped.sessionID != "" {
			// This goroutine will wait for the SSE connection to close
			go func(sessionID string) {
				<-r.Context().Done()
				// SSE connection closed
				log.Printf("🔌 [SSE] Client disconnected from SSE endpoint (session: %s)\n", sessionID)
				handleSessionClosed(sessionID)
			}(wrapped.sessionID)
		}
	} else {
		// For other requests (like /message endpoints), just pass through
		h.sseServer.ServeHTTP(w, r)
	}
}

// responseWrapper captures the session ID from SSE responses
type responseWrapper struct {
	http.ResponseWriter
	request   *http.Request
	sessionID string
	written   bool
}

// Write captures the session ID from the initial SSE response
func (w *responseWrapper) Write(p []byte) (int, error) {
	if !w.written && strings.Contains(string(p), "endpoint") {
		// Try to extract session ID from the endpoint message
		// Format: data: {"endpoint":"/mcp/message/{sessionId}"}
		if start := strings.Index(string(p), "/mcp/message/"); start != -1 {
			start += len("/mcp/message/")
			if end := strings.IndexAny(string(p[start:]), "\"}\n"); end != -1 {
				w.sessionID = string(p[start : start+end])
				log.Printf("🔗 [SSE] Client connected to SSE endpoint (session: %s)\n", w.sessionID)
			}
		}
		w.written = true
	}
	return w.ResponseWriter.Write(p)
}

// StartSSEServer starts the MCP server in SSE mode
func StartSSEServer(mcpServer *server.MCPServer, config SSEServerConfig) error {
	log.Printf("Starting Sidekick in SSE mode on %s:%s\n", config.Host, config.Port)
	
	// Create SSE server with session cleanup
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s:%s", config.Host, config.Port)),
		server.WithStaticBasePath("/mcp"),
		server.WithKeepAlive(true),
	)
	
	// Store SSE server globally for session tracking
	globalSSEServer = sseServer
	
	// Create a custom handler that wraps the SSE server
	handler := &sseHandler{
		sseServer: sseServer,
	}
	
	// Start HTTP server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Printf("SSE server listening on %s\n", addr)
	log.Printf("SSE endpoint: http://%s/mcp/sse\n", addr)
	
	// Create HTTP server
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	
	// Start server in a goroutine to handle graceful shutdown
	errChan := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()
	
	// Wait for either error or shutdown signal
	select {
	case err := <-errChan:
		return fmt.Errorf("SSE server error: %w", err)
	case <-shutdownChan:
		log.Println("Shutting down SSE server...")
		
		// Shutdown SSE server first (kills all session processes)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		if err := sseServer.Shutdown(ctx); err != nil {
			log.Printf("SSE server shutdown error: %v\n", err)
		}
		
		// Then shutdown HTTP server
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v\n", err)
		}
		
		return nil
	}
}


// handleSessionClosed is called when an SSE session is closed
func handleSessionClosed(sessionID string) {
	log.Printf("🔌 [SSE] Session %s disconnected, cleaning up...\n", sessionID)
	
	// Kill all processes associated with this session
	killedCount := registry.killProcessesBySession(sessionID)
	
	if killedCount > 0 {
		log.Printf("🧹 [SSE] Killed %d processes for disconnected session %s\n", killedCount, sessionID)
	} else {
		log.Printf("🧹 [SSE] No processes to clean up for session %s\n", sessionID)
	}
	
	// Remove session from manager
	sessionManager.RemoveSession(sessionID)
}