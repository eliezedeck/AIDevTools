package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestIntegrationWithRealServer tests the bridge against a real sidekick server
// This test is skipped by default and requires a running sidekick server
func TestIntegrationWithRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if sidekick server is running
	resp, err := exec.Command("curl", "-s", "-I", "http://localhost:5051/mcp/sse").Output()
	if err != nil {
		t.Skip("Sidekick server not running on localhost:5051, skipping integration test")
	}
	if !strings.Contains(string(resp), "200 OK") && !strings.Contains(string(resp), "405 Method Not Allowed") {
		t.Skip("Sidekick server not responding correctly, skipping integration test")
	}

	// Test the actual stdio2sse binary
	testCases := []struct {
		name     string
		input    string
		expectID interface{}
	}{
		{
			name:     "Initialize",
			input:    `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"integration-test","version":"1.0.0"}}}`,
			expectID: float64(1),
		},
		{
			name:     "List Tools",
			input:    `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
			expectID: float64(2),
		},
		{
			name:     "Register Specialist",
			input:    `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"register_specialist","arguments":{"name":"Integration Test Specialist","specialty":"testing","root_dir":"/tmp/test"}}}`,
			expectID: float64(3),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Build the stdio2sse binary
			buildCmd := exec.Command("go", "build", "-o", "stdio2sse_test", ".")
			if err := buildCmd.Run(); err != nil {
				t.Fatalf("Failed to build stdio2sse: %v", err)
			}
			defer exec.Command("rm", "-f", "stdio2sse_test").Run()

			// Start stdio2sse
			cmd := exec.Command("./stdio2sse_test", "--sse-url", "http://localhost:5051/mcp/sse", "--verbose")

			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatalf("Failed to create stdin pipe: %v", err)
			}

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("Failed to create stdout pipe: %v", err)
			}

			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatalf("Failed to create stderr pipe: %v", err)
			}

			if err := cmd.Start(); err != nil {
				t.Fatalf("Failed to start stdio2sse: %v", err)
			}
			defer cmd.Process.Kill()

			// Monitor stderr for errors
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					t.Logf("[stderr] %s", scanner.Text())
				}
			}()

			// Give it time to connect
			time.Sleep(2 * time.Second)

			// Send the test request
			if _, err := fmt.Fprintf(stdin, "%s\n", tc.input); err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}

			// Read response with timeout
			responseChan := make(chan string, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				if scanner.Scan() {
					responseChan <- scanner.Text()
				}
			}()

			select {
			case response := <-responseChan:
				t.Logf("Received response: %s", response)

				// Parse and validate response
				var responseObj JSONRPCMessage
				if err := json.Unmarshal([]byte(response), &responseObj); err != nil {
					t.Fatalf("Failed to parse JSON response: %v", err)
				}

				if responseObj.JSONRPC != "2.0" {
					t.Errorf("Expected JSONRPC 2.0, got %s", responseObj.JSONRPC)
				}

				if responseObj.ID != tc.expectID {
					t.Errorf("Expected ID %v, got %v", tc.expectID, responseObj.ID)
				}

				// Check for errors
				if responseObj.Error != nil {
					t.Logf("Response contains error (may be expected): %v", responseObj.Error)
				}

			case <-time.After(10 * time.Second):
				t.Fatalf("Timeout waiting for response")
			}
		})
	}
}

// TestIntegrationHangingScenario tests the specific hanging scenario that was causing issues
func TestIntegrationHangingScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if sidekick server is running
	resp, err := exec.Command("curl", "-s", "-I", "http://localhost:5051/mcp/sse").Output()
	if err != nil {
		t.Skip("Sidekick server not running on localhost:5051, skipping integration test")
	}
	if !strings.Contains(string(resp), "200 OK") && !strings.Contains(string(resp), "405 Method Not Allowed") {
		t.Skip("Sidekick server not responding correctly, skipping integration test")
	}

	// Build the stdio2sse binary
	buildCmd := exec.Command("go", "build", "-o", "stdio2sse_test", ".")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build stdio2sse: %v", err)
	}
	defer exec.Command("rm", "-f", "stdio2sse_test").Run()

	// Start stdio2sse
	cmd := exec.Command("./stdio2sse_test", "--sse-url", "http://localhost:5051/mcp/sse", "--verbose")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start stdio2sse: %v", err)
	}
	defer cmd.Process.Kill()

	// Monitor stderr
	var stderrOutput bytes.Buffer
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line + "\n")
			t.Logf("[stderr] %s", line)
		}
	}()

	// Give it time to connect
	time.Sleep(2 * time.Second)

	// Reproduce the exact scenario from the original error
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"debug-client","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"register_specialist","arguments":{"name":"Debug System planner debugging","specialty":"debugging","root_dir":"/Users/elie/Projects/Fiaranow-cloudflare"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_next_question","arguments":{"wait":true,"timeout":0}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_processes","arguments":{}}}`,
	}

	responses := make([]string, 0)
	responseChan := make(chan string, 10)

	// Start reading responses
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			responseChan <- scanner.Text()
		}
	}()

	// Send requests with delays
	for i, request := range requests {
		t.Logf("Sending request %d: %s", i+1, request)
		if _, err := fmt.Fprintf(stdin, "%s\n", request); err != nil {
			t.Fatalf("Failed to send request %d: %v", i+1, err)
		}

		// For the hanging request, don't wait too long
		timeout := 5 * time.Second
		if i == 2 { // get_next_question
			timeout = 2 * time.Second
		}

		// Try to get response
		select {
		case response := <-responseChan:
			responses = append(responses, response)
			t.Logf("Received response %d: %s", i+1, response)
		case <-time.After(timeout):
			if i == 2 {
				t.Logf("Request %d timed out as expected (hanging request)", i+1)
			} else {
				t.Logf("Request %d timed out", i+1)
			}
		}
	}

	// Wait a bit more for any delayed responses
	time.Sleep(3 * time.Second)

	// Collect any remaining responses
	for {
		select {
		case response := <-responseChan:
			responses = append(responses, response)
			t.Logf("Received delayed response: %s", response)
		default:
			goto done
		}
	}

done:
	t.Logf("Total responses received: %d", len(responses))

	// Analyze responses
	jsonErrors := 0
	responseIDs := make(map[float64]bool)

	for i, response := range responses {
		var responseObj JSONRPCMessage
		if err := json.Unmarshal([]byte(response), &responseObj); err != nil {
			jsonErrors++
			t.Errorf("JSON parse error in response %d: %v", i+1, err)
			t.Errorf("Raw response: %s", response)
		} else {
			if id, ok := responseObj.ID.(float64); ok {
				responseIDs[id] = true
			}
		}
	}

	// The key test: verify no JSON parsing errors
	if jsonErrors > 0 {
		t.Errorf("JSON parsing errors detected: %d", jsonErrors)
	}

	// Verify we got responses for non-hanging requests
	expectedIDs := []float64{1, 2, 4} // ID 3 might hang
	for _, id := range expectedIDs {
		if !responseIDs[id] && id != 3 { // Allow ID 3 to be missing
			t.Errorf("Missing response for request ID: %.0f", id)
		}
	}

	// Most importantly: verify that request 4 worked even if request 3 hung
	if !responseIDs[4] {
		t.Error("Request 4 failed - hanging request blocked subsequent requests!")
	} else {
		t.Log("SUCCESS: Request 4 worked even with hanging request 3 - bug is fixed!")
	}
}
