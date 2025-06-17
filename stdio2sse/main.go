package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Version can be set at build time using -ldflags "-X main.version=x.x.x"
var version = "dev"

// JSONRPCMessage represents a generic JSON-RPC message
type JSONRPCMessage struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// AsyncStdioBridge handles the bridging between stdio and SSE with async support
type AsyncStdioBridge struct {
	sseURL          string
	httpClient      *http.Client
	stdin           *bufio.Reader
	stdout          io.Writer
	mutex           sync.Mutex
	verbose         bool
	sessionID       string
	messageURL      string
	pendingRequests map[interface{}]chan JSONRPCMessage
	requestMutex    sync.RWMutex
}

func main() {
	// Handle command-line flags
	versionFlag := flag.Bool("version", false, "Print version and exit")
	sseURL := flag.String("sse-url", "", "SSE server URL to connect to (required)")
	bridgeName := flag.String("name", "SSE Bridge", "Name for the stdio bridge server")
	bridgeVersion := flag.String("bridge-version", "1.0.0", "Version for the stdio bridge server")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("stdio2sse %s\n", version)
		os.Exit(0)
	}

	if *sseURL == "" {
		fmt.Fprintf(os.Stderr, "Error: --sse-url is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// Set up logging
	if !*verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(0)
	} else {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Create the bridge
	bridge := &AsyncStdioBridge{
		sseURL:          *sseURL,
		httpClient:      &http.Client{Timeout: 0}, // No timeout for HTTP client
		stdin:           bufio.NewReader(os.Stdin),
		stdout:          os.Stdout,
		verbose:         *verbose,
		pendingRequests: make(map[interface{}]chan JSONRPCMessage),
	}

	// Initialize and run the bridge
	if err := bridge.Run(ctx, *bridgeName, *bridgeVersion); err != nil {
		fmt.Fprintf(os.Stderr, "Bridge error: %v\n", err)
		os.Exit(1)
	}
}

func (b *AsyncStdioBridge) Run(ctx context.Context, name, version string) error {
	log.Printf("Starting async stdio bridge connected to %s\n", b.sseURL)

	// Test SSE server connectivity
	if err := b.testSSEConnection(); err != nil {
		return fmt.Errorf("failed to connect to SSE server: %w", err)
	}

	// Start SSE listener for responses
	go b.listenSSE(ctx)

	// Main message processing loop
	for {
		select {
		case <-ctx.Done():
			log.Println("Context cancelled, shutting down...")
			return nil
		default:
			// Read a line from stdin
			line, err := b.stdin.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					log.Println("Stdin closed, shutting down...")
					return nil
				}
				return fmt.Errorf("failed to read from stdin: %w", err)
			}

			// Process the message asynchronously
			go b.processMessage(ctx, line)
		}
	}
}

func (b *AsyncStdioBridge) testSSEConnection() error {
	// Try to connect to the SSE endpoint to verify it's available
	req, err := http.NewRequest("GET", b.sseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to SSE server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("SSE server returned status %d", resp.StatusCode)
	}

	log.Printf("Successfully connected to SSE server")
	return nil
}

func (b *AsyncStdioBridge) listenSSE(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Connect to SSE endpoint (without sessionId - let server create one)
			req, err := http.NewRequestWithContext(ctx, "GET", b.sseURL, nil)
			if err != nil {
				log.Printf("Failed to create SSE request: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			resp, err := b.httpClient.Do(req)
			if err != nil {
				log.Printf("Failed to connect to SSE: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Read SSE events
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if b.verbose {
					log.Printf("SSE line: %s", line)
				}

				// Handle endpoint event (contains session ID and message URL)
				if strings.HasPrefix(line, "event: endpoint") {
					// Next line should contain the message URL
					if scanner.Scan() {
						dataLine := scanner.Text()
						if strings.HasPrefix(dataLine, "data: ") {
							messageURL := strings.TrimPrefix(dataLine, "data: ")
							b.messageURL = messageURL
							log.Printf("Received message endpoint: %s", messageURL)
						}
					}
				} else if strings.HasPrefix(line, "event: message") {
					// Next line should contain the JSON response
					if scanner.Scan() {
						dataLine := scanner.Text()
						if strings.HasPrefix(dataLine, "data: ") {
							data := strings.TrimPrefix(dataLine, "data: ")
							b.handleSSEMessage(data)
						}
					}
				}
			}

			resp.Body.Close()

			// If we get here, the SSE connection was closed
			log.Printf("SSE connection closed, reconnecting...")
			time.Sleep(1 * time.Second)
		}
	}
}

func (b *AsyncStdioBridge) handleSSEMessage(data string) {
	if b.verbose {
		log.Printf("Received SSE message: %s", data)
	}

	var message JSONRPCMessage
	if err := json.Unmarshal([]byte(data), &message); err != nil {
		log.Printf("Failed to parse SSE message: %v", err)
		return
	}

	// If this is a response to a pending request, send it to the waiting goroutine
	if message.ID != nil {
		b.requestMutex.RLock()
		if ch, exists := b.pendingRequests[message.ID]; exists {
			select {
			case ch <- message:
				// Message sent successfully
			default:
				// Channel is full or closed
			}
		}
		b.requestMutex.RUnlock()
	}

	// Always send the message to stdout as well
	b.sendResponse(message)
}

func (b *AsyncStdioBridge) processMessage(ctx context.Context, messageBytes []byte) {
	// Trim whitespace
	messageBytes = []byte(strings.TrimSpace(string(messageBytes)))

	if len(messageBytes) == 0 {
		return
	}

	if b.verbose {
		log.Printf("Received message: %s", string(messageBytes))
	}

	// Parse the JSON-RPC message
	var message JSONRPCMessage
	if err := json.Unmarshal(messageBytes, &message); err != nil {
		log.Printf("Failed to parse JSON-RPC message: %v", err)
		return
	}

	// Forward to SSE server
	if err := b.forwardToSSE(ctx, messageBytes, message.ID); err != nil {
		// Send error response back to client
		errorResponse := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      message.ID,
			Error: map[string]interface{}{
				"code":    -32603,
				"message": fmt.Sprintf("SSE server error: %v", err),
			},
		}
		b.sendResponse(errorResponse)
	}
}

func (b *AsyncStdioBridge) forwardToSSE(ctx context.Context, messageBytes []byte, requestID interface{}) error {
	// Wait for message URL to be available
	for b.messageURL == "" {
		time.Sleep(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for message URL")
		default:
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.messageURL, strings.NewReader(string(messageBytes)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if b.verbose {
		log.Printf("Forwarding to SSE: %s", b.messageURL)
	}

	// Send request
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to SSE server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		responseBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE server returned status %d: %s", resp.StatusCode, string(responseBytes))
	}

	return nil
}

func (b *AsyncStdioBridge) sendResponse(response JSONRPCMessage) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal response: %v", err)
		return
	}

	if b.verbose {
		log.Printf("Sending response: %s", string(responseBytes))
	}

	// Write response with newline
	if _, err := fmt.Fprintf(b.stdout, "%s\n", string(responseBytes)); err != nil {
		log.Printf("Failed to write response: %v", err)
	}

	// Flush if possible
	if flusher, ok := b.stdout.(interface{ Flush() error }); ok {
		flusher.Flush()
	}
}
