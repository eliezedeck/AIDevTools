package main

import (
	"context"
	"flag"
	"fmt"
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

// Global Streamable HTTP server reference for session tracking
var globalStreamableHTTPServer *server.StreamableHTTPServer

// Global TUI manager reference for shutdown handling
var globalTUIManager *TUIManager

// Shutdown channel for coordinated shutdown
var shutdownChan = make(chan struct{})
var shutdownOnce sync.Once

// TUIState manages TUI state for mutual exclusivity with logging
type TUIState struct {
	active     bool
	crashed    bool
	recovering bool
	mu         sync.RWMutex
}

// Global TUI state instance
var tuiState = &TUIState{}

func (t *TUIState) SetActive(active bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = active
}

func (t *TUIState) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.active
}

func (t *TUIState) SetCrashed(crashed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.crashed = crashed
}

func (t *TUIState) IsCrashed() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.crashed
}

func (t *TUIState) SetRecovering(recovering bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recovering = recovering
}

func (t *TUIState) IsRecovering() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.recovering
}

// attemptTUIRecovery attempts to restart TUI after a crash with auto-navigation to logs
func attemptTUIRecovery() {
	if tuiState.IsRecovering() {
		return // Already attempting recovery
	}

	tuiState.SetRecovering(true)
	defer tuiState.SetRecovering(false)

	EmergencyLog("TUI", "Attempting TUI auto-recovery after crash...")

	// Wait a moment for things to settle
	time.Sleep(1 * time.Second)

	// Create a new TUI manager for recovery
	recoveryManager := NewTUIManager()
	globalTUIManager = recoveryManager

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Recovery failed - don't try again
				tuiState.SetActive(false)
				tuiState.SetCrashed(true)
				ForceTerminalReset()
				EmergencyLog("TUI", "TUI recovery failed", fmt.Sprintf("Recovery panic: %v", r))
				return
			}
		}()

		// Small delay before recovery attempt
		time.Sleep(500 * time.Millisecond)
		tuiState.SetActive(true)
		tuiState.SetCrashed(false)

		// Log the recovery attempt
		LogError("TUI", "TUI crashed and is being recovered - switching to logs page")

		if err := recoveryManager.Start(); err != nil {
			tuiState.SetActive(false)
			tuiState.SetCrashed(true)
			ForceTerminalReset()
			EmergencyLog("TUI", "TUI recovery failed to start", err.Error())
		} else {
			tuiState.SetActive(false)
			LogInfo("TUI", "TUI recovery completed successfully")
		}

		// Trigger shutdown after recovery attempt
		shutdownOnce.Do(func() {
			close(shutdownChan)
		})
	}()
}

func main() {
	// Handle command-line flags
	versionFlag := flag.Bool("version", false, "Print version and exit")
	sseMode := flag.Bool("sse", true, "Run in SSE mode instead of stdio (default: true)")
	tuiMode := flag.Bool("tui", true, "Enable TUI mode (default: true, only available with --sse)")
	processesMode := flag.Bool("processes", false, "Enable process management tools (default: false)")
	port := flag.String("port", "5050", "Port for SSE server (default: 5050)")
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

	// 🔧 Define and register process management tools (only if enabled)
	if *processesMode {
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
	}

	// 🤝 Define agent communication tools

	answerQuestionTool := mcp.NewTool(
		"answer_question",
		mcp.WithDescription("Provide an answer to a question. A question can only be answered once and only once."),
		mcp.WithString("question_id",
			mcp.Required(),
			mcp.Description("Question ID to answer"),
		),
		mcp.WithString("answer",
			mcp.Required(),
			mcp.Description("Answer to the question"),
		),
	)

	getNextQuestionTool := mcp.NewTool(
		"get_next_question",
		mcp.WithDescription("Wait for and retrieve the next question for this specialist. Creates or joins a directory for the specified specialty. Blocks if no questions are available."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Agent name"),
		),
		mcp.WithString("specialty",
			mcp.Required(),
			mcp.Description("Specialty area (e.g., 'codebase', 'testing', 'security', 'flutter', 'convex', 'firebase-backend')"),
		),
		mcp.WithString("root_dir",
			mcp.Required(),
			mcp.Description("Root directory of the project"),
		),
		mcp.WithString("instructions",
			mcp.Description("Usage instructions for potential questioners (optional)"),
		),
		mcp.WithBoolean("wait",
			mcp.Description("Whether to wait for a question (default: true)"),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Timeout in milliseconds (optional, 0 = no timeout)"),
		),
	)

	askSpecialistTool := mcp.NewTool(
		"ask_specialist",
		mcp.WithDescription("Ask a question to a specialist agent. IMPORTANT: Always call list_specialists first to verify a specialist exists for the specialty and root_dir, otherwise this call will fail. If wait=true (default), blocks until answer is available."),
		mcp.WithString("specialty",
			mcp.Required(),
			mcp.Description("Specialty to ask - must match an active specialist from list_specialists"),
		),
		mcp.WithString("root_dir",
			mcp.Required(),
			mcp.Description("Root directory - must match an active specialist from list_specialists"),
		),
		mcp.WithString("question",
			mcp.Required(),
			mcp.Description("Question to ask"),
		),
		mcp.WithBoolean("wait",
			mcp.Description("Whether to wait for the answer (default: true). Recommended: use wait=true in most cases to get the answer immediately. Only set wait=false if you are doing other work in parallel and will call get_answer later to retrieve the response."),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Timeout in milliseconds if wait=true (optional, default 0 = no timeout)"),
		),
	)

	listSpecialistsTool := mcp.NewTool(
		"list_specialists",
		mcp.WithDescription("List all active specialist agents. MUST be called before ask_specialist to verify a specialist is available for your specialty and root_dir."),
	)

	getAnswerTool := mcp.NewTool(
		"get_answer",
		mcp.WithDescription("Get the answer for a previously asked question. If answer is not yet available, waits until it is (respecting timeout if provided)."),
		mcp.WithString("question_id",
			mcp.Required(),
			mcp.Description("ID of the previously asked question"),
		),
		mcp.WithNumber("timeout",
			mcp.Description("How long to wait for an answer in milliseconds (optional, default 0 = no timeout)"),
		),
	)

	getSystemHealthTool := mcp.NewTool(
		"get_system_health",
		mcp.WithDescription("Get diagnostic information about the Q&A system health, including active waiters and channel status."),
	)

	// 🔗 Register agent communication tools
	s.AddTool(answerQuestionTool, handleAnswerQuestion)
	s.AddTool(getNextQuestionTool, handleGetNextQuestion)
	s.AddTool(askSpecialistTool, handleAskSpecialist)
	s.AddTool(listSpecialistsTool, handleListSpecialists)
	s.AddTool(getAnswerTool, handleGetAnswer)
	s.AddTool(getSystemHealthTool, handleGetSystemHealth)

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
				// Set up comprehensive panic recovery for TUI startup
				defer func() {
					if r := recover(); r != nil {
						// TUI startup panic - perform emergency cleanup
						tuiState.SetActive(false)
						tuiState.SetCrashed(true)
						ForceTerminalReset()
						EmergencyLog("TUI", "TUI startup panic", fmt.Sprintf("%v", r))

						// Attempt auto-recovery instead of immediate shutdown
						EmergencyLog("TUI", "Attempting TUI auto-recovery after startup panic...")
						go attemptTUIRecovery()
						return
					}
				}()

				// Small delay to ensure SSE server is started first
				time.Sleep(200 * time.Millisecond)
				tuiState.SetActive(true)

				if err := tuiManager.Start(); err != nil {
					// TUI failed to start - ensure proper cleanup
					tuiState.SetActive(false)
					tuiState.SetCrashed(true)
					ForceTerminalReset()
					EmergencyLog("TUI", "TUI failed to start", err.Error())

					// Attempt auto-recovery
					EmergencyLog("TUI", "Attempting TUI auto-recovery after start failure...")
					go attemptTUIRecovery()
					return
				} else {
					// TUI exited normally
					tuiState.SetActive(false)
					LogInfo("TUI", "TUI exited normally")
				}

				// TUI has exited, trigger shutdown of entire application
				LogInfo("TUI", "TUI exited, shutting down sidekick...")
				shutdownOnce.Do(func() {
					close(shutdownChan)
				})
			}()
		}

		// Handle shutdown in a separate goroutine with forced exit timeout
		go func() {
			select {
			case <-sigChan:
				// Handle OS signal (Ctrl+C, SIGTERM, etc.)
				shutdownOnce.Do(func() {
					close(shutdownChan)
				})
				if tuiManager != nil {
					tuiManager.Stop()
				}

				// Force exit after timeout to prevent hanging
				go func() {
					time.Sleep(5 * time.Second)
					LogWarn("Main", "Force exit after shutdown timeout")
					os.Exit(1)
				}()
			case <-shutdownChan:
				// Shutdown already initiated (e.g., from TUI exit)
				// Just return to let this goroutine exit
				return
			}
		}()

		// Start SSE server (blocks until shutdown)
		if err := StartSSEServer(s, config); err != nil {
			LogError("Main", "Failed to start SSE server", err.Error())
			os.Exit(1)
		}

		// SSE server has shut down - exit immediately
		// In TUI mode, processes were already force killed
		// In non-TUI mode, graceful shutdown was attempted
		os.Exit(0)
	} else {
		// Stdio mode
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

// getRunningProcesses returns all currently running or pending processes
// This includes pending delayed spawns that haven't started yet
func getRunningProcesses() []*ProcessTracker {
	processes := registry.getAllProcesses()
	var running []*ProcessTracker
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		status := tracker.Status
		tracker.Mutex.RUnlock()

		if status == StatusRunning || status == StatusPending {
			running = append(running, tracker)
		}
	}
	return running
}

// terminateProcesses sends SIGTERM to running processes and cancels pending delayed spawns
func terminateProcesses(processes []*ProcessTracker) {
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		hasProcess := tracker.Process != nil && tracker.Process.Process != nil
		cancelFunc := tracker.CancelFunc
		tracker.Mutex.RUnlock()

		if hasProcess {
			// Running process - send SIGTERM
			tracker.Mutex.RLock()
			err := terminateProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				if killErr := tracker.Process.Process.Kill(); killErr != nil {
					// Process may already be dead
				}
			}
			tracker.Mutex.RUnlock()
		} else if cancelFunc != nil {
			// Pending delayed spawn - cancel it
			cancelFunc()
		}
	}
}

// forceKillProcesses sends SIGKILL to running processes and cancels pending delayed spawns
func forceKillProcesses(processes []*ProcessTracker) {
	for _, tracker := range processes {
		tracker.Mutex.Lock()
		if tracker.Process != nil && tracker.Process.Process != nil &&
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			// Running process - force kill
			err := forceKillProcessGroup(tracker.Process.Process.Pid)
			if err != nil {
				if killErr := tracker.Process.Process.Kill(); killErr != nil {
					// Process may already be dead
				}
			}
			tracker.Status = StatusKilled
		} else if tracker.Status == StatusPending && tracker.CancelFunc != nil {
			// Pending delayed spawn - cancel and mark as killed
			tracker.CancelFunc()
			tracker.Status = StatusKilled
		}
		tracker.Mutex.Unlock()
	}
}

// countRunningProcesses returns how many of the provided processes are still running
func countRunningProcesses(processes []*ProcessTracker) int {
	count := 0
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		if tracker.Status == StatusRunning || tracker.Status == StatusPending {
			count++
		}
		tracker.Mutex.RUnlock()
	}
	return count
}

// handleTUIShutdown performs graceful shutdown with UI feedback
func handleTUIShutdown(tuiApp *TUIApp) {
	StopCleanupRoutine()

	modal := NewShutdownModal(tuiApp.app)
	modal.Show(tuiApp.pages)

	runningProcesses := getRunningProcesses()
	totalProcesses := len(runningProcesses)

	if totalProcesses == 0 {
		modal.UpdateProgress(0, 0)
		time.Sleep(100 * time.Millisecond)
		return
	}

	modal.UpdateProgress(totalProcesses, totalProcesses)
	terminateProcesses(runningProcesses)

	// Wait up to 3 seconds with progress updates
	deadline := time.Now().Add(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		remaining := countRunningProcesses(runningProcesses)
		modal.UpdateProgress(remaining, totalProcesses)
		if remaining == 0 {
			time.Sleep(200 * time.Millisecond)
			return
		}
	}

	// Force kill remaining
	forceKillProcesses(runningProcesses)
	modal.UpdateProgress(0, totalProcesses)
	time.Sleep(300 * time.Millisecond)
}

// handleGracefulShutdown sends SIGTERM to all running processes, waits up to 5 seconds,
// then sends SIGKILL to any remaining processes
func handleGracefulShutdown() {
	StopCleanupRoutine()

	runningProcesses := getRunningProcesses()
	if len(runningProcesses) == 0 {
		return
	}

	terminateProcesses(runningProcesses)

	// Wait up to 5 seconds for graceful termination
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countRunningProcesses(runningProcesses) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill remaining
	forceKillProcesses(runningProcesses)
}
