package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// SSEServerConfig holds configuration for the SSE server
type SSEServerConfig struct {
	Host string
	Port string
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
	
	// Start session monitor goroutine
	go monitorSSESessions()
	
	// Start HTTP server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Printf("SSE server listening on %s\n", addr)
	log.Printf("SSE endpoint: http://%s/mcp/sse\n", addr)
	
	// Create HTTP server
	httpServer := &http.Server{
		Addr:    addr,
		Handler: sseServer,
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

// monitorSSESessions periodically checks for closed sessions and cleans up their processes
func monitorSSESessions() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Check each tracked session to see if it's still active
			sessions := sessionManager.GetAllSessions()
			for sessionID, session := range sessions {
				// If the session's context is done, it means the session is closed
				select {
				case <-session.Context.Done():
					// Session is closed, trigger cleanup
					handleSessionClosed(sessionID)
				default:
					// Session is still active
				}
			}
		case <-shutdownChan:
			// Server is shutting down
			return
		}
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