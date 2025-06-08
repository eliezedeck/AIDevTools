package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Version can be set at build time using -ldflags "-X main.version=x.x.x"
var version = "dev"

// Global SSE server reference for session tracking
var globalSSEServer *server.SSEServer

// Global TUI manager reference for shutdown handling
var globalTUIManager *TUIManager

// Shutdown channel for coordinated shutdown
var shutdownChan = make(chan struct{})
var shutdownOnce sync.Once

// TUI state tracking for mutual exclusivity with logging
var (
	isTUIActive bool
	tuiMutex    sync.RWMutex
)

// setTUIActive safely sets the TUI active state
func setTUIActive(active bool) {
	tuiMutex.Lock()
	defer tuiMutex.Unlock()
	isTUIActive = active
}

// isTUIActiveCheck safely checks if TUI is active
func isTUIActiveCheck() bool {
	tuiMutex.RLock()
	defer tuiMutex.RUnlock()
	return isTUIActive
}

// logIfNotTUI logs only when SSE server is running but TUI is not active
func logIfNotTUI(format string, args ...interface{}) {
	if globalSSEServer != nil && !isTUIActiveCheck() {
		log.Printf(format, args...)
	}
}

func main() {
	// Handle command-line flags
	versionFlag := flag.Bool("version", false, "Print version and exit")
	sseMode := flag.Bool("sse", false, "Run in SSE mode instead of stdio")
	tuiMode := flag.Bool("tui", false, "Enable TUI mode (only available with --sse)")
	port := flag.String("port", "8080", "Port for SSE server (default: 8080)")
	host := flag.String("host", "localhost", "Host for SSE server (default: localhost)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("sidekick %s\n", version)
		os.Exit(0)
	}

	// Validate flags
	if *tuiMode && !*sseMode {
		fmt.Println("Error: TUI mode (--tui) is only available with SSE mode (--sse)")
		os.Exit(1)
	}

	// 🛠️ Create hooks for session lifecycle management
	hooks := &server.Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, session server.ClientSession) {
		sessionID := session.SessionID()
		handleSessionClosed(sessionID)
	})

	// 🛠️ Create a new MCP server
	s := server.NewMCPServer(
		"Sidekick Notifications",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithHooks(hooks),
	)

	// 🗣️ Define and register the notifications_speak tool (macOS only)
	if runtime.GOOS == "darwin" {
		speakTool := mcp.NewTool(
			"notifications_speak",
			mcp.WithDescription("Play a system sound and speak the provided text (max 50 words)"),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to speak (max 50 words)"),
			),
		)
		s.AddTool(speakTool, handleSpeak)
	}

	// 🔧 Define process management tools
	spawnProcessTool := mcp.NewTool(
		"spawn_process",
		mcp.WithDescription("Spawn a new process and start tracking its output with configurable buffer size"),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Command to execute"),
		),
		mcp.WithArray("args",
			mcp.Description("Command arguments"),
		),
		mcp.WithString("working_dir",
			mcp.Description("Working directory (optional)"),
		),
		mcp.WithObject("env",
			mcp.Description("Environment variables (optional)"),
		),
		mcp.WithNumber("buffer_size",
			mcp.Description("Ring buffer size in bytes (default: 10MB)"),
		),
		mcp.WithBoolean("combine_output",
			mcp.Description("Whether to combine stdout and stderr into single stream (default: false)"),
		),
		mcp.WithNumber("delay",
			mcp.Description("Delay in milliseconds before starting process (max: 300000 = 5 minutes). With sync_delay=false, returns immediately with 'pending' status and executes after delay. With sync_delay=true, waits for delay then starts process before returning with 'running' status"),
		),
		mcp.WithBoolean("sync_delay",
			mcp.Description("Controls delay behavior: false (default) = return immediately with 'pending' status, execute later; true = wait for delay, start process, then return with 'running' status"),
		),
		mcp.WithString("name",
			mcp.Description("Optional human-readable name for the process (non-unique)"),
		),
	)

	getPartialProcessOutputTool := mcp.NewTool(
		"get_partial_process_output",
		mcp.WithDescription("Get incremental output from a process since last read (tail -f functionality)"),
		mcp.WithString("process_id",
			mcp.Required(),
			mcp.Description("Process identifier"),
		),
		mcp.WithString("streams",
			mcp.Description("Which streams to read from"),
			mcp.Enum("stdout", "stderr", "both"),
		),
		mcp.WithNumber("max_lines",
			mcp.Description("Maximum lines to return (optional)"),
		),
		mcp.WithArray("filters",
			mcp.Description("Optional command pipeline - each element is [command, ...args]"),
		),
		mcp.WithNumber("delay",
			mcp.Description("Delay before returning output in milliseconds (max: 120000 = 2 minutes). Smart delay with early termination - if process completes during delay, returns immediately with output"),
		),
	)

	getFullProcessOutputTool := mcp.NewTool(
		"get_full_process_output",
		mcp.WithDescription("Get the complete output from a process (all data in memory)"),
		mcp.WithString("process_id",
			mcp.Required(),
			mcp.Description("Process identifier"),
		),
		mcp.WithString("streams",
			mcp.Description("Which streams to read from"),
			mcp.Enum("stdout", "stderr", "both"),
		),
		mcp.WithNumber("max_lines",
			mcp.Description("Maximum lines to return (optional)"),
		),
		mcp.WithArray("filters",
			mcp.Description("Optional command pipeline - each element is [command, ...args]"),
		),
		mcp.WithNumber("delay",
			mcp.Description("Delay before returning output in milliseconds (max: 120000 = 2 minutes). Smart delay with early termination - if process completes during delay, returns immediately with output"),
		),
	)

	sendProcessInputTool := mcp.NewTool(
		"send_process_input",
		mcp.WithDescription("Send input data to a running process's stdin"),
		mcp.WithString("process_id",
			mcp.Required(),
			mcp.Description("Process identifier"),
		),
		mcp.WithString("input",
			mcp.Required(),
			mcp.Description("Input data to send to process stdin"),
		),
		mcp.WithBoolean("auto_newline",
			mcp.Description("Automatically append newline character to input (default: true)"),
		),
	)

	listProcessesTool := mcp.NewTool(
		"list_processes",
		mcp.WithDescription("List all tracked processes and their status"),
	)

	killProcessTool := mcp.NewTool(
		"kill_process",
		mcp.WithDescription("Terminate a tracked process"),
		mcp.WithString("process_id",
			mcp.Required(),
			mcp.Description("Process identifier"),
		),
	)

	getProcessStatusTool := mcp.NewTool(
		"get_process_status",
		mcp.WithDescription("Get detailed status of a process"),
		mcp.WithString("process_id",
			mcp.Required(),
			mcp.Description("Process identifier"),
		),
	)

	spawnMultipleProcessesTool := mcp.NewTool(
		"spawn_multiple_processes",
		mcp.WithDescription("Spawn multiple processes sequentially with individual delays. Delays are cumulative (each delay occurs after previous process scheduled). In async mode (sync_delay=false for any process with delay>0), returns immediately - initial no-delay processes show 'running', first delayed process and all subsequent show 'pending'. In sync mode (all sync_delay=true), waits for all processes to start before returning with 'running' status"),
		mcp.WithArray("processes",
			mcp.Required(),
			mcp.Description("Array of process configurations. Each supports: command (required), args, name, working_dir, env, buffer_size, delay (ms), sync_delay (bool). Delays are sequential - process N waits for its delay after process N-1 is scheduled"),
		),
	)

	// 🔗 Register process management tools
	s.AddTool(spawnProcessTool, handleSpawnProcess)
	s.AddTool(spawnMultipleProcessesTool, handleSpawnMultipleProcesses)
	s.AddTool(getPartialProcessOutputTool, handleGetPartialProcessOutput)
	s.AddTool(getFullProcessOutputTool, handleGetFullProcessOutput)
	s.AddTool(sendProcessInputTool, handleSendProcessInput)
	s.AddTool(listProcessesTool, handleListProcesses)
	s.AddTool(killProcessTool, handleKillProcess)
	s.AddTool(getProcessStatusTool, handleGetProcessStatus)

	// 🚦 Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Signal handling is done inside each mode (SSE or stdio)

	// 🚦 Start the MCP server
	if *sseMode {
		// SSE mode
		config := SSEServerConfig{
			Host: *host,
			Port: *port,
		}
		
		// Start TUI if requested
		var tuiManager *TUIManager
		if *tuiMode {
			tuiManager = NewTUIManager()
			globalTUIManager = tuiManager // Store globally for shutdown handling
			go func() {
				// Small delay to ensure SSE server is started first
				time.Sleep(200 * time.Millisecond)
				setTUIActive(true)
				if err := tuiManager.Start(); err != nil {
					setTUIActive(false)
					log.Printf("TUI error: %v", err)
				}
				setTUIActive(false)
				// TUI has exited, trigger shutdown of entire application
				logIfNotTUI("TUI exited, shutting down sidekick...")
				shutdownOnce.Do(func() {
					close(shutdownChan)
				})
			}()
		}
		
		// Handle shutdown in a separate goroutine
		go func() {
			select {
			case <-sigChan:
				// Handle OS signal (Ctrl+C)
				shutdownOnce.Do(func() {
					close(shutdownChan)
				})
				if tuiManager != nil {
					tuiManager.Stop()
				}
			case <-shutdownChan:
				// Shutdown already initiated (e.g., from TUI exit)
				// Just return to let this goroutine exit
				return
			}
		}()
		
		// Start SSE server (blocks until shutdown)
		if err := StartSSEServer(s, config); err != nil {
			log.Fatalf("Failed to start SSE server: %v\n", err)
		}
		
		// SSE server has shut down - exit immediately
		// In TUI mode, processes were already force killed
		// In non-TUI mode, graceful shutdown was attempted
		os.Exit(0)
	} else {
		// Stdio mode (default)
		// Handle signals for stdio mode
		go func() {
			<-sigChan
			handleGracefulShutdown()
			os.Exit(0)
		}()
		
		if err := server.ServeStdio(s); err != nil {
			fmt.Printf("Server error: %v\n", err)
		}
	}
}

// handleTUIShutdown performs graceful shutdown with UI feedback
func handleTUIShutdown(tuiApp *TUIApp) {
	// Stop the cleanup routine first
	StopCleanupRoutine()
	
	// Create and show shutdown modal
	modal := NewShutdownModal(tuiApp.app)
	modal.Show(tuiApp.pages)
	
	// Get all tracked processes
	processes := registry.getAllProcesses()
	
	// Filter to only running processes
	var runningProcesses []*ProcessTracker
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		if tracker.Process != nil && tracker.Process.Process != nil &&
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			runningProcesses = append(runningProcesses, tracker)
		}
		tracker.Mutex.RUnlock()
	}
	
	totalProcesses := len(runningProcesses)
	if totalProcesses == 0 {
		// No processes to terminate
		modal.UpdateProgress(0, 0)
		time.Sleep(100 * time.Millisecond) // Brief pause to show the modal
		return
	}
	
	// Initial modal update
	modal.UpdateProgress(totalProcesses, totalProcesses)
	
	// Send SIGTERM to all running processes
	for _, tracker := range runningProcesses {
		tracker.Mutex.RLock()
		if tracker.Process != nil && tracker.Process.Process != nil {
			// Send graceful termination signal
			err := terminateProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				// If platform-specific termination fails, use standard process.Kill()
				_ = tracker.Process.Process.Kill()
			}
		}
		tracker.Mutex.RUnlock()
	}
	
	// Give processes up to 3 seconds to terminate gracefully
	deadline := time.Now().Add(3 * time.Second)
	checkInterval := 100 * time.Millisecond
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	
	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			// Count remaining processes
			remainingCount := 0
			for _, tracker := range runningProcesses {
				tracker.Mutex.RLock()
				if tracker.Status == StatusRunning || tracker.Status == StatusPending {
					remainingCount++
				}
				tracker.Mutex.RUnlock()
			}
			
			// Update modal
			modal.UpdateProgress(remainingCount, totalProcesses)
			
			if remainingCount == 0 {
				// All processes terminated
				time.Sleep(200 * time.Millisecond) // Brief pause to show success
				return
			}
		}
	}
	
	// Force kill any remaining processes
	remainingCount := 0
	for _, tracker := range runningProcesses {
		tracker.Mutex.Lock()
		if tracker.Process != nil && tracker.Process.Process != nil &&
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			// Force kill the entire process group (Unix) or process (Windows)
			err := forceKillProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				// If platform-specific force kill fails, use standard process.Kill()
				_ = tracker.Process.Process.Kill()
			}
			tracker.Status = StatusKilled
			remainingCount++
		}
		tracker.Mutex.Unlock()
	}
	
	// Final update showing force kill
	if remainingCount > 0 {
		modal.UpdateProgress(0, totalProcesses)
		time.Sleep(300 * time.Millisecond) // Show final state briefly
	}
}

// handleGracefulShutdown sends SIGTERM to all running processes, waits up to 5 seconds,
// then sends SIGKILL to any remaining processes
func handleGracefulShutdown() {
	// Stop the cleanup routine first
	StopCleanupRoutine()
	
	// Get all tracked processes
	processes := registry.getAllProcesses()

	// Send termination signal to all running process groups
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		if tracker.Process != nil && tracker.Process.Process != nil &&
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			// Terminate the entire process group (Unix) or process (Windows)
			err := terminateProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				// If platform-specific termination fails, use standard process.Kill()
				_ = tracker.Process.Process.Kill()
			}
		}
		tracker.Mutex.RUnlock()
	}

	// Give processes up to 5 seconds to terminate gracefully
	deadline := time.Now().Add(5 * time.Second)
	checkInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		allTerminated := true

		for _, tracker := range processes {
			tracker.Mutex.RLock()
			if tracker.Status == StatusRunning || tracker.Status == StatusPending {
				allTerminated = false
			}
			tracker.Mutex.RUnlock()

			if !allTerminated {
				break
			}
		}

		if allTerminated {
			return
		}

		time.Sleep(checkInterval)
	}

	// Force kill any remaining process groups
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		if tracker.Process != nil && tracker.Process.Process != nil &&
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			// Force kill the entire process group (Unix) or process (Windows)
			err := forceKillProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				// If platform-specific force kill fails, use standard process.Kill()
				_ = tracker.Process.Process.Kill()
			}
		}
		tracker.Mutex.RUnlock()
	}
}
