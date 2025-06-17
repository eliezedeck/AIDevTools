package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockSSEServer simulates the sidekick SSE server for testing
type MockSSEServer struct {
	server     *httptest.Server
	sessions   map[string]*MockSession
	mutex      sync.RWMutex
	messageLog []string
}

type MockSession struct {
	ID         string
	MessageURL string
	Messages   chan JSONRPCMessage
}

func NewMockSSEServer() *MockSSEServer {
	mock := &MockSSEServer{
		sessions:   make(map[string]*MockSession),
		messageLog: make([]string, 0),
	}

	mux := http.NewServeMux()

	// SSE endpoint
	mux.HandleFunc("/mcp/sse", mock.handleSSE)

	// Message endpoint
	mux.HandleFunc("/mcp/message", mock.handleMessage)

	mock.server = httptest.NewServer(mux)
	return mock
}

func (m *MockSSEServer) Close() {
	m.server.Close()
}

func (m *MockSSEServer) URL() string {
	return m.server.URL + "/mcp/sse"
}

func (m *MockSSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Create new session
	sessionID := fmt.Sprintf("test-session-%d", time.Now().UnixNano())
	messageURL := fmt.Sprintf("%s/mcp/message?sessionId=%s", m.server.URL, sessionID)

	session := &MockSession{
		ID:         sessionID,
		MessageURL: messageURL,
		Messages:   make(chan JSONRPCMessage, 10),
	}

	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Send endpoint event
	fmt.Fprintf(w, "event: endpoint\n")
	fmt.Fprintf(w, "data: %s\n\n", messageURL)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Keep connection alive and send messages
	ctx := r.Context()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Send keepalive
			fmt.Fprintf(w, "event: message\n")
			fmt.Fprintf(w, "data: \n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case msg := <-session.Messages:
			// Send actual message
			msgBytes, _ := json.Marshal(msg)
			fmt.Fprintf(w, "event: message\n")
			fmt.Fprintf(w, "data: %s\n\n", string(msgBytes))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func (m *MockSSEServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")

	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Read the message
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Log the message
	m.mutex.Lock()
	m.messageLog = append(m.messageLog, string(body))
	m.mutex.Unlock()

	// Parse the request
	var request JSONRPCMessage
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Create mock response based on method
	response := m.createMockResponse(request)

	// Send response via SSE
	select {
	case session.Messages <- response:
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "Session channel full", http.StatusServiceUnavailable)
	}
}

func (m *MockSSEServer) createMockResponse(request JSONRPCMessage) JSONRPCMessage {
	switch request.Method {
	case "initialize":
		return JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo": map[string]interface{}{
					"name":    "Mock Sidekick Server",
					"version": "1.0.0",
				},
			},
		}
	case "tools/call":
		params := request.Params.(map[string]interface{})
		toolName := params["name"].(string)

		switch toolName {
		case "register_specialist":
			return JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result: map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "Specialist registered successfully"},
					},
				},
			}
		case "get_next_question":
			// Simulate a hanging call that eventually times out
			return JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      request.ID,
				Error: map[string]interface{}{
					"code":    -32603,
					"message": "No questions available",
				},
			}
		case "list_processes":
			return JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result: map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "[]"},
					},
				},
			}
		default:
			return JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      request.ID,
				Error: map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("Unknown tool: %s", toolName),
				},
			}
		}
	default:
		return JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error: map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Unknown method: %s", request.Method),
			},
		}
	}
}

func (m *MockSSEServer) GetMessageLog() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]string, len(m.messageLog))
	copy(result, m.messageLog)
	return result
}

// TestBridgeBasicFunctionality tests basic bridge operations
func TestBridgeBasicFunctionality(t *testing.T) {
	// Start mock SSE server
	mockServer := NewMockSSEServer()
	defer mockServer.Close()

	// Create bridge
	var stdout bytes.Buffer
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}` + "\n")

	bridge := &AsyncStdioBridge{
		sseURL:          mockServer.URL(),
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		stdin:           bufio.NewReader(stdin),
		stdout:          &stdout,
		verbose:         true,
		pendingRequests: make(map[interface{}]chan JSONRPCMessage),
	}

	// Test connection
	err := bridge.testSSEConnection()
	if err != nil {
		t.Fatalf("Failed to connect to mock SSE server: %v", err)
	}

	// Start bridge in background
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- bridge.Run(ctx, "test-bridge", "1.0.0")
	}()

	// Wait for response
	time.Sleep(3 * time.Second)

	// Check that we got a response
	output := stdout.String()
	if output == "" {
		t.Fatal("No output received from bridge")
	}

	// Verify JSON response
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("No response lines received")
	}

	var response JSONRPCMessage
	if err := json.Unmarshal([]byte(lines[0]), &response); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if response.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC 2.0, got %s", response.JSONRPC)
	}

	if response.ID != float64(1) { // JSON unmarshals numbers as float64
		t.Errorf("Expected ID 1, got %v", response.ID)
	}
}
