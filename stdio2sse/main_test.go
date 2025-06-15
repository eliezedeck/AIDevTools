package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStdioBridge(t *testing.T) {
	// Step 1: Build stdio2sse
	t.Log("Building stdio2sse...")
	buildCmd := exec.Command("go", "build", "-o", "stdio2sse", "main.go")
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build stdio2sse: %v\nOutput: %s", err, output)
	}
	t.Log("✓ Build successful")
	
	// Step 2: Start stdio2sse with SSE URL (without verbose for cleaner stdio)
	t.Log("Starting stdio2sse...")
	cmd := exec.Command("./stdio2sse", "--sse-url", "http://localhost:5050/mcp/sse")
	
	// Set up pipes for stdio communication
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	
	// Capture stderr for debugging (logs go to stderr)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}
	
	// Start the process
	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start stdio2sse: %v", err)
	}
	t.Log("✓ Started stdio2sse")
	
	// Give it a moment to connect to SSE server
	time.Sleep(500 * time.Millisecond)
	
	// Step 3: Send Initialize request
	t.Log("Sending Initialize request...")
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	
	requestBytes, err := json.Marshal(initRequest)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	
	// Send request (with newline for JSON-RPC over stdio)
	_, err = fmt.Fprintf(stdin, "%s\n", requestBytes)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	t.Log("✓ Sent Initialize request")
	
	// Create a channel for responses
	responseChan := make(chan map[string]interface{}, 10)
	
	// Read responses from stdout in a goroutine
	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var response map[string]interface{}
			if err := decoder.Decode(&response); err != nil {
				t.Logf("Decoder error: %v", err)
				return
			}
			responseChan <- response
		}
	}()
	
	// Wait for Initialize response
	select {
	case response := <-responseChan:
		t.Logf("Initialize response: %+v", response)
		if response["error"] != nil {
			t.Fatalf("Initialize failed: %v", response["error"])
		}
		result := response["result"].(map[string]interface{})
		serverInfo := result["serverInfo"].(map[string]interface{})
		t.Logf("✓ Initialized with server: %s v%s", serverInfo["name"], serverInfo["version"])
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for Initialize response")
	}
	
	// Step 4: Send ListTools request
	t.Log("Sending ListTools request...")
	listToolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	
	requestBytes, err = json.Marshal(listToolsRequest)
	if err != nil {
		t.Fatalf("Failed to marshal ListTools request: %v", err)
	}
	
	_, err = fmt.Fprintf(stdin, "%s\n", requestBytes)
	if err != nil {
		t.Fatalf("Failed to send ListTools request: %v", err)
	}
	
	// Wait for ListTools response
	select {
	case response := <-responseChan:
		if response["error"] != nil {
			t.Fatalf("ListTools failed: %v", response["error"])
		}
		result := response["result"].(map[string]interface{})
		tools := result["tools"].([]interface{})
		t.Logf("✓ Found %d tools", len(tools))
		
		// Check for spawn_process tool
		foundSpawnProcess := false
		for _, tool := range tools {
			toolMap := tool.(map[string]interface{})
			if toolMap["name"] == "spawn_process" {
				foundSpawnProcess = true
				break
			}
		}
		if !foundSpawnProcess {
			t.Fatal("spawn_process tool not found")
		}
		t.Log("✓ Found spawn_process tool")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ListTools response")
	}
	
	// Step 5: Call spawn_process with "ls -l"
	t.Log("Calling spawn_process with 'ls -l'...")
	spawnRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "spawn_process",
			"arguments": map[string]interface{}{
				"command": "ls",
				"args":    []string{"-l"},
			},
		},
	}
	
	requestBytes, err = json.Marshal(spawnRequest)
	if err != nil {
		t.Fatalf("Failed to marshal spawn_process request: %v", err)
	}
	
	_, err = fmt.Fprintf(stdin, "%s\n", requestBytes)
	if err != nil {
		t.Fatalf("Failed to send spawn_process request: %v", err)
	}
	
	// Wait for spawn_process response
	var processID string
	select {
	case response := <-responseChan:
		if response["error"] != nil {
			t.Fatalf("spawn_process failed: %v", response["error"])
		}
		result := response["result"].(map[string]interface{})
		t.Logf("spawn_process result: %+v", result)
		
		content := result["content"].([]interface{})
		if len(content) > 0 {
			firstContent := content[0].(map[string]interface{})
			text := firstContent["text"].(string)
			t.Logf("Response text: %s", text)
			
			// Try to parse as JSON first
			var processInfo map[string]interface{}
			if err := json.Unmarshal([]byte(text), &processInfo); err == nil {
				if pid, ok := processInfo["process_id"].(string); ok {
					processID = pid
				}
			} else {
				// Fallback to text parsing
				if strings.Contains(text, "process_id:") {
					parts := strings.Split(text, "process_id:")
					if len(parts) > 1 {
						processID = strings.TrimSpace(strings.Split(parts[1], "\n")[0])
					}
				}
			}
		}
		
		if processID == "" {
			t.Fatal("Failed to extract process ID from response")
		}
		t.Logf("✓ Process spawned with ID: %s", processID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for spawn_process response")
	}
	
	// Step 6: Get process output
	t.Log("Getting process output...")
	getOutputRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "get_partial_process_output",
			"arguments": map[string]interface{}{
				"process_id": processID,
				"delay":      500, // Wait 500ms to ensure ls completes
			},
		},
	}
	
	requestBytes, err = json.Marshal(getOutputRequest)
	if err != nil {
		t.Fatalf("Failed to marshal get_output request: %v", err)
	}
	
	_, err = fmt.Fprintf(stdin, "%s\n", requestBytes)
	if err != nil {
		t.Fatalf("Failed to send get_output request: %v", err)
	}
	
	// Wait for output response
	select {
	case response := <-responseChan:
		if response["error"] != nil {
			t.Fatalf("get_partial_process_output failed: %v", response["error"])
		}
		result := response["result"].(map[string]interface{})
		content := result["content"].([]interface{})
		if len(content) > 0 {
			firstContent := content[0].(map[string]interface{})
			text := firstContent["text"].(string)
			
			// Parse the JSON response
			var outputInfo map[string]interface{}
			if err := json.Unmarshal([]byte(text), &outputInfo); err != nil {
				t.Fatalf("Failed to parse output response: %v", err)
			}
			
			stdout := outputInfo["stdout"].(string)
			status := outputInfo["status"].(string)
			exitCode := outputInfo["exit_code"].(float64)
			
			t.Logf("Process status: %s, exit code: %.0f", status, exitCode)
			t.Logf("Process stdout:\n%s", stdout)
			
			// Check if output contains directory listing
			if strings.Contains(stdout, "sidekick") && strings.Contains(stdout, "stdio2sse") {
				t.Log("✓ Successfully received directory listing with expected directories")
			} else {
				t.Fatalf("Output doesn't contain expected directories")
			}
			
			if status == "completed" && exitCode == 0 {
				t.Log("✓ Process completed successfully")
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for output response")
	}
	
	// Read initial output in a goroutine to see connection status
	outputChan := make(chan string, 10)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				outputChan <- string(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()
	
	// Collect output for a short time
	timeout := time.After(2 * time.Second)
	var allOutput string
	
	for {
		select {
		case output := <-outputChan:
			allOutput += output
			t.Logf("Bridge output: %s", output)
		case <-timeout:
			t.Log("Timeout reached, checking final state...")
			goto done
		}
	}
	
done:
	// Clean shutdown
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Warning: Failed to kill process: %v", err)
	}
	t.Log("✓ Killed stdio2sse")
	
	// Check if connection succeeded
	if strings.Contains(allOutput, "Connected to SSE server") {
		t.Log("✓ Successfully connected to SSE server")
	} else if strings.Contains(allOutput, "failed") || strings.Contains(allOutput, "error") {
		t.Fatalf("Failed to connect to SSE server. Output:\n%s", allOutput)
	}
}