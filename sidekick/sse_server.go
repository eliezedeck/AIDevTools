package main

import (
	"context"
	"fmt"
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
	LogInfo("SSEServer", "Starting Sidekick in SSE mode", fmt.Sprintf("Host: %s, Port: %s", config.Host, config.Port))

	// Create SSE server with session cleanup
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s:%s", config.Host, config.Port)),
		server.WithStaticBasePath("/mcp"),
		server.WithKeepAlive(true),
	)

	// Store SSE server globally for session tracking
	globalSSEServer = sseServer

	// Start HTTP server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	LogInfo("SSEServer", "SSE server listening", fmt.Sprintf("Address: %s", addr))
	LogInfo("SSEServer", "SSE endpoint available", fmt.Sprintf("URL: http://%s/mcp/sse", addr))

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    addr,
		Handler: sseServer, // Use SSE server directly
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
		LogInfo("SSEServer", "Shutting down SSE server...")

		// If TUI is active, use graceful shutdown with UI feedback
		if isTUIActiveCheck() && globalTUIManager != nil && globalTUIManager.app != nil {
			// Graceful shutdown for TUI mode with modal UI
			handleTUIShutdown(globalTUIManager.app)
			
			// Immediately close the HTTP server after graceful shutdown
			httpServer.Close()
			
			// No additional shutdown attempts needed
			return nil
		} else {
			// Normal graceful shutdown for non-TUI mode
			go handleGracefulShutdown()

			// Immediately disable keep-alives and stop accepting new connections
			httpServer.SetKeepAlivesEnabled(false)
			
			// Force close all active connections after a short grace period
			go func() {
				time.Sleep(1 * time.Second)
				httpServer.Close()
			}()

			// Try graceful shutdown with very short timeout
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer shutdownCancel()
			
			// Shutdown SSE server first
			if err := sseServer.Shutdown(shutdownCtx); err != nil {
				LogError("SSEServer", "SSE server shutdown error", err.Error())
			}
			
			// Then shutdown HTTP server (will likely be already closed by force close)
			if err := httpServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
				LogError("SSEServer", "HTTP server shutdown error", err.Error())
			}

			return nil
		}
	}
}

// handleSessionClosed is called when an SSE session is closed
func handleSessionClosed(sessionID string) {
	LogInfo("SSEServer", "Session disconnected, cleaning up", fmt.Sprintf("SessionID: %s", sessionID))

	// Mark session as disconnected (but keep it in memory)
	sessionManager.MarkSessionDisconnected(sessionID)

	// Clean up any specialists registered by this session
	agentQARegistry.CleanupForSession(sessionID)

	// Kill all processes associated with this session
	killedCount := registry.killProcessesBySession(sessionID)

	if killedCount > 0 {
		LogInfo("SSEServer", "Processes killed for disconnected session", 
			fmt.Sprintf("Count: %d, SessionID: %s", killedCount, sessionID))
	} else {
		LogInfo("SSEServer", "No processes to clean up", fmt.Sprintf("SessionID: %s", sessionID))
	}
}
