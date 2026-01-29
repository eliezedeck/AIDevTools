package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/server"
)

// FlexibleSessionIdManager generates plain UUIDs and validates by extracting the UUID via regex.
// It accepts any session ID that contains a valid UUID, regardless of prefix.
// It only validates format, not existence, to allow reconnects after disconnect/restart.
type FlexibleSessionIdManager struct{}

// uuidRegex matches a UUID anywhere in the string
var uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// Generate creates a new session ID (plain UUID)
func (s *FlexibleSessionIdManager) Generate() string {
	return uuid.New().String()
}

// Validate checks if the session ID contains a valid UUID anywhere in the string.
// This handles any prefix format (e.g., "mcp-session-<uuid>", "<uuid>", "prefix-<uuid>", etc.)
func (s *FlexibleSessionIdManager) Validate(sessionID string) (isTerminated bool, err error) {
	// Extract UUID from anywhere in the session ID
	if !uuidRegex.MatchString(sessionID) {
		return false, fmt.Errorf("invalid session id (no valid UUID found): %s", sessionID)
	}

	// Don't check existence - allow reconnects after disconnect/restart
	return false, nil
}

// Terminate marks a session as terminated (no-op since we don't track state)
func (s *FlexibleSessionIdManager) Terminate(sessionID string) (isNotAllowed bool, err error) {
	return false, nil
}

// responseWriterWrapper captures the status code from the response
// and preserves optional interfaces like http.Flusher and http.Hijacker
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
}

func newResponseWriterWrapper(w http.ResponseWriter) *responseWriterWrapper {
	return &responseWriterWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default to 200
	}
}

func (rww *responseWriterWrapper) WriteHeader(code int) {
	if rww.headerWritten {
		return // Prevent double WriteHeader calls
	}
	rww.statusCode = code
	rww.headerWritten = true
	rww.ResponseWriter.WriteHeader(code)
}

func (rww *responseWriterWrapper) Write(b []byte) (int, error) {
	if !rww.headerWritten {
		rww.headerWritten = true
	}
	return rww.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it
func (rww *responseWriterWrapper) Flush() {
	if flusher, ok := rww.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker if the underlying ResponseWriter supports it
func (rww *responseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rww.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijack")
}

// Push implements http.Pusher if the underlying ResponseWriter supports it
func (rww *responseWriterWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := rww.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// bodyCapturingReader wraps an io.ReadCloser to capture bytes as they're read
type bodyCapturingReader struct {
	reader    io.ReadCloser
	buffer    *bytes.Buffer
	maxBytes  int
	truncated bool
}

func newBodyCapturingReader(r io.ReadCloser, maxBytes int) *bodyCapturingReader {
	return &bodyCapturingReader{
		reader:   r,
		buffer:   &bytes.Buffer{},
		maxBytes: maxBytes,
	}
}

func (bcr *bodyCapturingReader) Read(p []byte) (int, error) {
	n, err := bcr.reader.Read(p)
	if n > 0 {
		if bcr.buffer.Len() >= bcr.maxBytes {
			// Buffer already full, mark as truncated
			bcr.truncated = true
		} else {
			remaining := bcr.maxBytes - bcr.buffer.Len()
			if n <= remaining {
				bcr.buffer.Write(p[:n])
			} else {
				bcr.buffer.Write(p[:remaining])
				bcr.truncated = true
			}
		}
	}
	return n, err
}

func (bcr *bodyCapturingReader) Close() error {
	return bcr.reader.Close()
}

func (bcr *bodyCapturingReader) CapturedBody() string {
	if bcr.truncated {
		return bcr.buffer.String() + "... [truncated]"
	}
	return bcr.buffer.String()
}

// formatHeaders builds a headers string for logging (no redaction - full details for debugging)
func formatHeaders(headers http.Header) string {
	var builder strings.Builder
	for name, values := range headers {
		fmt.Fprintf(&builder, "  %s: %s\n", name, strings.Join(values, ", "))
	}
	return builder.String()
}

// loggingMiddleware wraps an http.Handler to log full request details on errors only
func loggingMiddleware(next http.Handler, transport string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap the body to capture it as the handler reads it (only up to 8KB for errors)
		var bodyCapture *bodyCapturingReader
		if r.Body != nil {
			bodyCapture = newBodyCapturingReader(r.Body, 8192)
			r.Body = bodyCapture
		}

		// Wrap response writer to capture status code
		wrappedWriter := newResponseWriterWrapper(w)

		// Call the actual handler
		next.ServeHTTP(wrappedWriter, r)

		// Only log on errors - full unredacted details for debugging
		if wrappedWriter.statusCode >= 400 {
			// Build full URL with query params
			fullURL := r.URL.Path
			if r.URL.RawQuery != "" {
				fullURL = r.URL.Path + "?" + r.URL.RawQuery
			}

			headersStr := formatHeaders(r.Header)
			var bodyStr string
			if bodyCapture != nil {
				bodyStr = bodyCapture.CapturedBody()
			}

			LogError(transport, fmt.Sprintf("HTTP %d", wrappedWriter.statusCode),
				fmt.Sprintf("\n=== REQUEST DUMP ===\nMethod: %s\nURL: %s\nRemoteAddr: %s\nContentLength: %d\n\nHeaders:\n%s\nBody:\n%s\n=== END REQUEST DUMP ===",
					r.Method, fullURL, r.RemoteAddr, r.ContentLength, headersStr, bodyStr))
		}
	})
}

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
	// Use FlexibleSessionIdManager which:
	// - Generates plain UUIDs (no prefix)
	// - Validates by stripping "mcp-session-" prefix if present (handles Codex adding prefix)
	// - Only validates format, not existence (allows reconnects after disconnect/restart)
	streamableHTTPServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithSessionIdManager(&FlexibleSessionIdManager{}),
		server.WithHeartbeatInterval(30*time.Second), // Keep connection alive
	)

	// Store servers globally for session tracking
	globalSSEServer = sseServer
	globalStreamableHTTPServer = streamableHTTPServer

	// Create combined handler for both transports
	// Use http.StripPrefix for StreamableHTTP since WithEndpointPath only works with Start()
	// Wrap with logging middleware for debugging HTTP errors
	streamableHTTPWithLogging := loggingMiddleware(
		http.StripPrefix("/mcp", streamableHTTPServer),
		"StreamableHTTP",
	)
	handler := &combinedHandler{
		sseServer:                     sseServer,
		streamableHTTPServer:          streamableHTTPServer,
		streamableHTTPStrippedHandler: streamableHTTPWithLogging,
	}

	LogInfo("HTTPServer", "SSE endpoint available", fmt.Sprintf("URL: http://%s/mcp/sse", addr))
	LogInfo("HTTPServer", "Streamable HTTP endpoint available", fmt.Sprintf("URL: http://%s/mcp", addr))

	// Create HTTP server with combined handler
	// Set very large timeouts (24 hours) to support long-running tool calls like get_next_question
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  24 * time.Hour,
		WriteTimeout: 24 * time.Hour,
		IdleTimeout:  24 * time.Hour,
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
