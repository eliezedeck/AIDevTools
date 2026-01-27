package main

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// SSEServerConfig holds configuration for the HTTP server
type SSEServerConfig struct {
	Host string
	Port string
}

// combinedHandler routes requests to either SSE or Streamable HTTP transport
type combinedHandler struct {
	sseServer                   *server.SSEServer
	streamableHTTPServer        *server.StreamableHTTPServer
	streamableHTTPStrippedHandler http.Handler
}

func (h *combinedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route to SSE server for SSE-specific endpoints
	// SSE uses: GET /mcp/sse for event stream, POST /mcp/message for messages
	if strings.HasPrefix(path, "/mcp/sse") || strings.HasPrefix(path, "/mcp/message") {
		h.sseServer.ServeHTTP(w, r)
		return
	}

	// Route to Streamable HTTP server for /mcp endpoint (exact match only)
	// Streamable HTTP uses: POST /mcp for all operations
	// We use http.StripPrefix to remove /mcp since WithEndpointPath only works with Start()
	if path == "/mcp" {
		if r.Method == http.MethodPost || r.Method == http.MethodGet || r.Method == http.MethodDelete {
			h.streamableHTTPStrippedHandler.ServeHTTP(w, r)
			return
		}
	}

	// Fallback: try SSE server for any other /mcp paths
	if strings.HasPrefix(path, "/mcp") {
		h.sseServer.ServeHTTP(w, r)
		return
	}

	// Not found for non-MCP paths
	http.NotFound(w, r)
}

// StartSSEServer starts the MCP server with both SSE and Streamable HTTP transports
func StartSSEServer(mcpServer *server.MCPServer, config SSEServerConfig) error {
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	LogInfo("HTTPServer", "Starting Sidekick HTTP server", fmt.Sprintf("Host: %s, Port: %s", config.Host, config.Port))

	// Create SSE server for SSE transport (Claude Code, etc.)
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s:%s", config.Host, config.Port)),
		server.WithStaticBasePath("/mcp"),
		server.WithKeepAlive(true),
	)

	// Create Streamable HTTP server for Streamable HTTP transport (Codex, etc.)
	// Note: WithEndpointPath only works with Start(), not when used as http.Handler
	// We handle routing in combinedHandler instead
	streamableHTTPServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithStateful(true),
		server.WithHeartbeatInterval(30*time.Second), // Keep connection alive
	)

	// Store servers globally for session tracking
	globalSSEServer = sseServer
	globalStreamableHTTPServer = streamableHTTPServer

	// Create combined handler for both transports
	// Use http.StripPrefix for StreamableHTTP since WithEndpointPath only works with Start()
	handler := &combinedHandler{
		sseServer:                     sseServer,
		streamableHTTPServer:          streamableHTTPServer,
		streamableHTTPStrippedHandler: http.StripPrefix("/mcp", streamableHTTPServer),
	}

	LogInfo("HTTPServer", "SSE endpoint available", fmt.Sprintf("URL: http://%s/mcp/sse", addr))
	LogInfo("HTTPServer", "Streamable HTTP endpoint available", fmt.Sprintf("URL: http://%s/mcp", addr))

	// Create HTTP server with combined handler
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
		return fmt.Errorf("HTTP server error: %w", err)
	case <-shutdownChan:
		LogInfo("HTTPServer", "Shutting down HTTP server...")

		// If TUI is active, use graceful shutdown with UI feedback
		if tuiState.IsActive() && globalTUIManager != nil && globalTUIManager.app != nil {
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

			// Shutdown SSE server
			if err := sseServer.Shutdown(shutdownCtx); err != nil {
				LogError("HTTPServer", "SSE server shutdown error", err.Error())
			}

			// Shutdown Streamable HTTP server
			if err := streamableHTTPServer.Shutdown(shutdownCtx); err != nil {
				LogError("HTTPServer", "Streamable HTTP server shutdown error", err.Error())
			}

			// Then shutdown HTTP server (will likely be already closed by force close)
			if err := httpServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
				LogError("HTTPServer", "HTTP server shutdown error", err.Error())
			}

			return nil
		}
	}
}

// handleSessionClosed is called when a session is closed
func handleSessionClosed(sessionID string) {
	LogInfo("HTTPServer", "Session disconnected, cleaning up", fmt.Sprintf("SessionID: %s", sessionID))

	// Mark session as disconnected (but keep it in memory)
	sessionManager.MarkSessionDisconnected(sessionID)

	// No need to clean up specialists in the new directory-based system

	// Kill all processes associated with this session
	killedCount := registry.killProcessesBySession(sessionID)

	if killedCount > 0 {
		LogInfo("HTTPServer", "Processes killed for disconnected session",
			fmt.Sprintf("Count: %d, SessionID: %s", killedCount, sessionID))
	} else {
		LogInfo("HTTPServer", "No processes to clean up", fmt.Sprintf("SessionID: %s", sessionID))
	}

	// Force garbage collection to clean up any hanging resources
	// This helps prevent memory leaks from disconnected sessions
	go func() {
		time.Sleep(1 * time.Second)
		runtime.GC()
	}()
}
