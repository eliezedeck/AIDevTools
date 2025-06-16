package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Version can be set at build time using -ldflags "-X main.version=x.x.x"
var version = "dev"

type StdioBridge struct {
	stdioServer *server.MCPServer
	sseClient   *client.Client
	sseURL      string
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
	bridge := &StdioBridge{
		sseURL: *sseURL,
	}

	// Initialize the bridge
	if err := bridge.Initialize(ctx, *bridgeName, *bridgeVersion); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize bridge: %v\n", err)
		os.Exit(1)
	}

	// Serve stdio
	log.Printf("Starting stdio bridge connected to %s\n", *sseURL)

	// Set up cleanup on exit
	defer func() {
		log.Println("Cleaning up stdio bridge...")
		if bridge.sseClient != nil {
			// Try to gracefully close the SSE client
			if err := bridge.sseClient.Close(); err != nil {
				log.Printf("Warning: Error closing SSE client: %v\n", err)
			}
		}
		log.Println("Stdio bridge cleanup complete")
	}()

	if err := server.ServeStdio(bridge.stdioServer); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func (b *StdioBridge) Initialize(ctx context.Context, name string, version string) error {
	// Create SSE client
	log.Printf("Connecting to SSE server at %s...\n", b.sseURL)

	// Create a new SSE client
	sseClient, err := client.NewSSEMCPClient(b.sseURL)
	if err != nil {
		return fmt.Errorf("failed to create SSE client: %w", err)
	}

	// Start the client
	if err := sseClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start SSE client: %w", err)
	}

	// Initialize the connection with a timeout
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create initialization request
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    "stdio2sse",
				Version: version,
			},
			Capabilities: mcp.ClientCapabilities{
				Experimental: map[string]interface{}{},
				Sampling:     nil,
			},
		},
	}

	// Initialize the client
	initResult, err := sseClient.Initialize(initCtx, initRequest)
	if err != nil {
		return fmt.Errorf("failed to initialize SSE client: %w", err)
	}

	log.Printf("Connected to SSE server: %s version %s\n",
		initResult.ServerInfo.Name,
		initResult.ServerInfo.Version)

	// Check if the server has tool capability
	if initResult.Capabilities.Tools == nil {
		log.Printf("Warning: SSE server does not advertise tool capability\n")
	}

	// List available tools from the SSE server
	toolsCtx, toolsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer toolsCancel()

	toolsResult, err := sseClient.ListTools(toolsCtx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("failed to list tools from SSE server: %w", err)
	}

	log.Printf("Found %d tools on SSE server\n", len(toolsResult.Tools))

	// Create stdio server
	b.stdioServer = server.NewMCPServer(
		name,
		version,
		server.WithToolCapabilities(false),
	)

	// Register each tool from the SSE server
	for _, tool := range toolsResult.Tools {
		// Make a copy of the tool for the closure
		toolCopy := tool

		// Create a proxy handler for this tool
		handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Update the request to use the correct tool name
			request.Params.Name = toolCopy.Name

			// Check if context is already cancelled before making the call
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
			default:
			}

			// Forward the call to the SSE server (no timeout - allow indefinite waits)
			result, err := sseClient.CallTool(ctx, request)
			if err != nil {
				// Check if it's a context cancellation error
				if ctx.Err() != nil {
					return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
				}
				return nil, fmt.Errorf("SSE server error: %w", err)
			}

			return result, nil
		}

		// Register the tool with our stdio server
		b.stdioServer.AddTool(toolCopy, handler)
	}

	// Store the client for later use
	b.sseClient = sseClient

	return nil
}
