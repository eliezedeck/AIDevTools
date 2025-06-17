package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBridgeStressTest performs stress testing with multiple concurrent requests
func TestBridgeStressTest(t *testing.T) {
	// Start mock SSE server
	mockServer := NewMockSSEServer()
	defer mockServer.Close()

	// Create input with multiple requests
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stress-test","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"register_specialist","arguments":{"name":"Test Specialist","specialty":"testing","root_dir":"/test"}}}`,
	}

	// Add many concurrent tool calls
	for i := 3; i < 53; i++ { // 50 additional requests
		requests = append(requests, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"list_processes","arguments":{}}}`,
			i,
		))
	}

	input := strings.Join(requests, "\n") + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	bridge := &AsyncStdioBridge{
		sseURL:          mockServer.URL(),
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		stdin:           bufio.NewReader(stdin),
		stdout:          &stdout,
		verbose:         false, // Reduce noise in stress test
		pendingRequests: make(map[interface{}]chan JSONRPCMessage),
	}

	// Test connection
	err := bridge.testSSEConnection()
	if err != nil {
		t.Fatalf("Failed to connect to mock SSE server: %v", err)
	}

	// Start bridge
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- bridge.Run(ctx, "stress-test-bridge", "1.0.0")
	}()

	// Wait for all responses
	time.Sleep(10 * time.Second)

	// Analyze results
	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	successCount := 0
	errorCount := 0
	jsonErrors := 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		var response JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			jsonErrors++
			t.Logf("JSON parse error: %v, line: %s", err, line)
			continue
		}

		if response.Error != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	t.Logf("Stress test results:")
	t.Logf("  Total requests sent: %d", len(requests))
	t.Logf("  Total responses received: %d", len(lines))
	t.Logf("  Successful responses: %d", successCount)
	t.Logf("  Error responses: %d", errorCount)
	t.Logf("  JSON parse errors: %d", jsonErrors)

	// Verify no JSON parsing errors occurred
	if jsonErrors > 0 {
		t.Errorf("JSON parsing errors detected: %d", jsonErrors)
	}

	// Verify we got responses for most requests
	if successCount+errorCount < len(requests)/2 {
		t.Errorf("Too few responses received: %d out of %d requests", successCount+errorCount, len(requests))
	}

	// Check message log from mock server
	messageLog := mockServer.GetMessageLog()
	t.Logf("Messages received by mock server: %d", len(messageLog))

	if len(messageLog) < len(requests)/2 {
		t.Errorf("Mock server received too few messages: %d out of %d", len(messageLog), len(requests))
	}
}

// TestBridgeHangingRequest tests that hanging requests don't block other requests
func TestBridgeHangingRequest(t *testing.T) {
	// Start mock SSE server
	mockServer := NewMockSSEServer()
	defer mockServer.Close()

	// Create input with a hanging request followed by normal requests
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"hang-test","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"register_specialist","arguments":{"name":"Test Specialist","specialty":"testing","root_dir":"/test"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_next_question","arguments":{"wait":true,"timeout":0}}}`, // This should hang
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_processes","arguments":{}}}`,                           // This should still work
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_processes","arguments":{}}}`,                           // This should still work
	}

	input := strings.Join(requests, "\n") + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	bridge := &AsyncStdioBridge{
		sseURL:          mockServer.URL(),
		httpClient:      &http.Client{Timeout: 10 * time.Second},
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

	// Start bridge
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- bridge.Run(ctx, "hang-test-bridge", "1.0.0")
	}()

	// Wait for responses
	time.Sleep(8 * time.Second)

	// Analyze results
	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	responseIDs := make(map[float64]bool)
	jsonErrors := 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		var response JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			jsonErrors++
			t.Logf("JSON parse error: %v, line: %s", err, line)
			continue
		}

		if id, ok := response.ID.(float64); ok {
			responseIDs[id] = true
			t.Logf("Received response for ID: %.0f", id)
		}
	}

	t.Logf("Hanging request test results:")
	t.Logf("  Total responses received: %d", len(lines))
	t.Logf("  JSON parse errors: %d", jsonErrors)
	t.Logf("  Response IDs received: %v", responseIDs)

	// Verify no JSON parsing errors occurred
	if jsonErrors > 0 {
		t.Errorf("JSON parsing errors detected: %d", jsonErrors)
	}

	// Verify we got responses for non-hanging requests
	expectedNonHangingIDs := []float64{1, 2, 4, 5} // ID 3 is the hanging request
	for _, id := range expectedNonHangingIDs {
		if !responseIDs[id] {
			t.Errorf("Missing response for non-hanging request ID: %.0f", id)
		}
	}

	// The key test: verify that requests after the hanging request still work
	if !responseIDs[4] || !responseIDs[5] {
		t.Error("Hanging request blocked subsequent requests - this is the bug we fixed!")
	}
}

// BenchmarkBridgePerformance benchmarks the bridge performance
func BenchmarkBridgePerformance(b *testing.B) {
	// Start mock SSE server
	mockServer := NewMockSSEServer()
	defer mockServer.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Create a simple request
		request := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"list_processes","arguments":{}}}`, i)
		stdin := strings.NewReader(request + "\n")
		var stdout bytes.Buffer

		bridge := &AsyncStdioBridge{
			sseURL:          mockServer.URL(),
			httpClient:      &http.Client{Timeout: 5 * time.Second},
			stdin:           bufio.NewReader(stdin),
			stdout:          &stdout,
			verbose:         false,
			pendingRequests: make(map[interface{}]chan JSONRPCMessage),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			bridge.Run(ctx, "bench-bridge", "1.0.0")
		}()

		// Wait for response
		time.Sleep(500 * time.Millisecond)
		cancel()
		wg.Wait()
	}
}
