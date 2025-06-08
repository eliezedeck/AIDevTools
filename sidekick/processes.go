package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

type ProcessStatus string

const (
	StatusPending   ProcessStatus = "pending"
	StatusRunning   ProcessStatus = "running"
	StatusCompleted ProcessStatus = "completed"
	StatusFailed    ProcessStatus = "failed"
	StatusKilled    ProcessStatus = "killed"
)

type ProcessTracker struct {
	ID            string         `json:"id"`
	Name          string         `json:"name,omitempty"`
	SessionID     string         `json:"session_id,omitempty"` // SSE session that owns this process
	PID           int            `json:"pid"`
	Command       string         `json:"command"`
	Args          []string       `json:"args"`
	WorkingDir    string         `json:"working_dir"`
	BufferSize    int64          `json:"buffer_size"`
	CombineOutput bool           `json:"combine_output"`
	DelayStart    time.Duration  `json:"delay_start"`
	SyncDelay     bool           `json:"sync_delay"`
	StartTime     time.Time      `json:"start_time"`
	LastAccessed  time.Time      `json:"last_accessed"`
	Status        ProcessStatus  `json:"status"`
	StdoutCursor  int64          `json:"stdout_cursor"`
	StderrCursor  int64          `json:"stderr_cursor"`
	StdoutBuffer  *RingBuffer    `json:"-"`
	StderrBuffer  *RingBuffer    `json:"-"`
	Process       *exec.Cmd      `json:"-"`
	StdinWriter   io.WriteCloser `json:"-"`
	ExitCode      *int           `json:"exit_code,omitempty"`
	Mutex         sync.RWMutex   `json:"-"`
}

type OutputResponse struct {
	ProcessID    string        `json:"process_id"`
	Stdout       string        `json:"stdout,omitempty"`
	Stderr       string        `json:"stderr,omitempty"`
	StdoutCursor int64         `json:"stdout_cursor"`
	StderrCursor int64         `json:"stderr_cursor"`
	Status       ProcessStatus `json:"status"`
	ExitCode     *int          `json:"exit_code,omitempty"`
}

type ProcessRegistry struct {
	processes map[string]*ProcessTracker
	mutex     sync.RWMutex
}

const (
	DefaultBufferSize  = 10 * 1024 * 1024 // 10MB default buffer size
	MaxOutputDelay     = 120000           // 2 minutes max delay for output tools
	MaxSpawnDelay      = 300000           // 5 minutes max delay for spawn_process
	DelayCheckInterval = 100              // Check process status every 100ms during delay
)

type RingBuffer struct {
	data       []byte
	maxSize    int64
	totalBytes int64
	mutex      sync.RWMutex
}

func NewRingBuffer(maxSize int64) *RingBuffer {
	return &RingBuffer{
		data:    make([]byte, 0),
		maxSize: maxSize,
	}
}

func (rb *RingBuffer) Write(data []byte) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	rb.data = append(rb.data, data...)
	rb.totalBytes += int64(len(data))

	// Trim from beginning if we exceed max size
	if int64(len(rb.data)) > rb.maxSize {
		excess := int64(len(rb.data)) - rb.maxSize
		rb.data = rb.data[excess:]
	}
}

func (rb *RingBuffer) GetContent() string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return string(rb.data)
}

func (rb *RingBuffer) GetContentFromCursor(cursor int64) string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	// Calculate effective position in current buffer
	discardedBytes := rb.totalBytes - int64(len(rb.data))
	effectivePos := cursor - discardedBytes

	effectivePos = max(effectivePos, 0)
	if effectivePos >= int64(len(rb.data)) {
		return ""
	}

	return string(rb.data[effectivePos:])
}

func (rb *RingBuffer) Len() int {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return len(rb.data)
}

func (rb *RingBuffer) TotalBytes() int64 {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.totalBytes
}

// Whitelist of allowed filter commands for security
var allowedCommands = map[string]bool{
	// Text Search & Pattern Matching
	"grep": true, // search text using patterns
	"rg":   true, // ripgrep - faster grep alternative with better defaults
	"awk":  true, // pattern scanning and processing language
	"sed":  true, // stream editor for filtering and transforming text

	// Text Extraction & Display
	"head": true, // output first part of files
	"tail": true, // output last part of files
	"cut":  true, // extract columns from lines
	"cat":  true, // concatenate and display files
	"tee":  true, // read from input and write to output and files

	// Text Sorting & Deduplication
	"sort": true, // sort lines of text files
	"uniq": true, // report or omit repeated lines

	// Text Analysis & Counting
	"wc": true, // word, line, character, and byte count

	// Text Transformation
	"tr":       true, // translate or delete characters
	"column":   true, // columnate lists
	"fold":     true, // wrap each input line to fit specified width
	"expand":   true, // convert tabs to spaces
	"unexpand": true, // convert spaces to tabs
	"nl":       true, // number lines of files
	"paste":    true, // merge lines of files

	// JSON Processing
	"jq": true, // command-line JSON processor

	// Binary Data & Encoding
	"od":       true, // octal dump - display files in various formats
	"hexdump":  true, // ASCII, decimal, hexadecimal, octal dump
	"base64":   true, // base64 encode/decode data
	"uuencode": true, // encode binary file for transmission
	"uudecode": true, // decode a file created by uuencode
}

// Filter timeout - prevent hanging commands
const filterTimeout = 10 * time.Second

// filterOutput applies a pipeline of commands to filter the input
func filterOutput(input string, commands [][]string) (string, error) {
	if len(commands) == 0 {
		return input, nil
	}

	// Validate all commands first
	for i, cmdArray := range commands {
		if len(cmdArray) == 0 {
			return "", fmt.Errorf("command %d is empty", i)
		}

		program := cmdArray[0]
		if !allowedCommands[program] {
			return "", fmt.Errorf("command not allowed: %s", program)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), filterTimeout)
	defer cancel()

	currentInput := input

	// Apply each command in the pipeline
	for i, cmdArray := range commands {
		program := cmdArray[0]
		args := cmdArray[1:]

		cmd := exec.CommandContext(ctx, program, args...)
		cmd.Stdin = strings.NewReader(currentInput)
		
		// Run command and get output
		output, err := cmd.CombinedOutput()
		
		// In bash pipes, the output is always passed to the next command,
		// regardless of exit code (unless the command completely fails to execute)
		if err != nil {
			// Check if this is an ExitError (command ran but exited non-zero)
			// vs other errors (command not found, etc.)
			if _, ok := err.(*exec.ExitError); ok {
				// Command executed but returned non-zero exit code
				// In bash pipes, this doesn't stop the pipeline
				// Examples: grep returns 1 when no matches, diff returns 1 when files differ
				currentInput = string(output)
				continue
			}
			
			// This is a real error (command not found, permission denied, etc.)
			if ctx.Err() == context.DeadlineExceeded {
				return currentInput, fmt.Errorf("filter command %d (%s) timed out", i, program)
			}
			return currentInput, fmt.Errorf("filter command %d (%s) failed: %v", i, program, err)
		}

		// Command succeeded (exit code 0)
		currentInput = string(output)
	}

	return currentInput, nil
}

var (
	registry = &ProcessRegistry{
		processes: make(map[string]*ProcessTracker),
	}
	cleanupInterval = 15 * time.Minute
	processTimeout  = 1 * time.Hour
)

func init() {
	go startCleanupRoutine()
}

func startCleanupRoutine() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		cleanupStaleProcesses()
	}
}

func cleanupStaleProcesses() {
	now := time.Now()
	var staleProcesses []string

	// First pass: identify stale processes
	registry.mutex.RLock()
	for id, tracker := range registry.processes {
		tracker.Mutex.RLock()
		isStale := now.Sub(tracker.LastAccessed) > processTimeout
		tracker.Mutex.RUnlock()

		if isStale {
			staleProcesses = append(staleProcesses, id)
		}
	}
	registry.mutex.RUnlock()

	// Second pass: remove stale processes using the proper method
	for _, id := range staleProcesses {
		registry.removeProcess(id)
	}
}

func (r *ProcessRegistry) addProcess(tracker *ProcessTracker) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.processes[tracker.ID] = tracker
}

func (r *ProcessRegistry) getProcess(id string) (*ProcessTracker, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	tracker, exists := r.processes[id]
	if exists {
		tracker.Mutex.Lock()
		tracker.LastAccessed = time.Now()
		tracker.Mutex.Unlock()
	}
	return tracker, exists
}

func (r *ProcessRegistry) getAllProcesses() []*ProcessTracker {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	processes := make([]*ProcessTracker, 0, len(r.processes))
	now := time.Now()

	for _, tracker := range r.processes {
		tracker.Mutex.Lock()
		tracker.LastAccessed = now
		tracker.Mutex.Unlock()
		processes = append(processes, tracker)
	}

	return processes
}

func (r *ProcessRegistry) removeProcess(id string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.processes, id)
}

// killProcessesBySession kills all processes associated with a session
func (r *ProcessRegistry) killProcessesBySession(sessionID string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	killedCount := 0
	for _, tracker := range r.processes {
		tracker.Mutex.RLock()
		if tracker.SessionID == sessionID && 
			(tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			tracker.Mutex.RUnlock()
			
			// Kill the process
			tracker.Mutex.Lock()
			if tracker.Process != nil && tracker.Process.Process != nil {
				// Try graceful termination first
				err := terminateProcessGroup(tracker.Process.Process.Pid)
				if err != nil {
					// Fallback to standard kill
					_ = tracker.Process.Process.Kill()
				}
				tracker.Status = StatusKilled
				killedCount++
				
				// Log session cleanup kill
				logMsg := fmt.Sprintf("🧹 [SSE] Process killed (session cleanup): %s", tracker.Command)
				if tracker.Name != "" {
					logMsg += fmt.Sprintf(" (name: %s)", tracker.Name)
				}
				logMsg += fmt.Sprintf(" (PID: %d, ID: %s)", tracker.PID, tracker.ID)
				logIfNotTUI(logMsg)
			} else if tracker.Status == StatusPending {
				// Cancel pending processes
				tracker.Status = StatusKilled
				killedCount++
				
				// Log cancelled pending process
				logMsg := fmt.Sprintf("🚫 [SSE] Pending process cancelled (session cleanup): %s", tracker.Command)
				if tracker.Name != "" {
					logMsg += fmt.Sprintf(" (name: %s)", tracker.Name)
				}
				logMsg += fmt.Sprintf(" (ID: %s)", tracker.ID)
				logIfNotTUI(logMsg)
			}
			tracker.Mutex.Unlock()
		} else {
			tracker.Mutex.RUnlock()
		}
	}
	
	return killedCount
}

// executeDelayedProcess actually starts the process after any delay
func executeDelayedProcess(ctx context.Context, tracker *ProcessTracker, envVars map[string]string) error {
	// Use background context for the process to avoid it being killed when request context is cancelled
	cmd := exec.CommandContext(context.Background(), tracker.Command, tracker.Args...)
	if tracker.WorkingDir != "" {
		cmd.Dir = tracker.WorkingDir
	}

	// Configure process group for proper cleanup
	configureProcessGroup(cmd)

	env := os.Environ()
	env = append(env, "NO_COLOR=1", "TERM=dumb")
	for k, v := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		tracker.Mutex.Lock()
		tracker.Status = StatusFailed
		tracker.Mutex.Unlock()
		return fmt.Errorf("failed to create stdin pipe: %v", err)
	}

	if tracker.CombineOutput {
		// When combining output, redirect both stdout and stderr to the same buffer
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to create stdout pipe: %v", err)
		}

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to create stderr pipe: %v", err)
		}

		if err := cmd.Start(); err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to start process: %v", err)
		}

		tracker.Mutex.Lock()
		tracker.Process = cmd
		tracker.PID = cmd.Process.Pid
		tracker.StdinWriter = stdinPipe
		tracker.Status = StatusRunning
		
		// Log process start (SSE mode only)
		logMsg := fmt.Sprintf("🚀 Process started: %s", tracker.Command)
		if len(tracker.Args) > 0 {
			logMsg += fmt.Sprintf(" %v", tracker.Args)
		}
		logMsg += fmt.Sprintf(" (PID: %d, ID: %s", tracker.PID, tracker.ID)
		if tracker.Name != "" {
			logMsg += fmt.Sprintf(", name: %s", tracker.Name)
		}
		if tracker.SessionID != "" {
			logMsg += fmt.Sprintf(", session: %s", tracker.SessionID)
		}
		logMsg += ")"
		logIfNotTUI(logMsg)
		
		tracker.Mutex.Unlock()

		// Stream both stdout and stderr to the same buffer (chronological order preserved)
		go streamToRingBuffer(stdoutPipe, tracker.StdoutBuffer)
		go streamToRingBuffer(stderrPipe, tracker.StdoutBuffer)
	} else {
		// Separate output streams
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to create stdout pipe: %v", err)
		}

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to create stderr pipe: %v", err)
		}

		if err := cmd.Start(); err != nil {
			tracker.Mutex.Lock()
			tracker.Status = StatusFailed
			tracker.Mutex.Unlock()
			return fmt.Errorf("failed to start process: %v", err)
		}

		tracker.Mutex.Lock()
		tracker.Process = cmd
		tracker.PID = cmd.Process.Pid
		tracker.StdinWriter = stdinPipe
		tracker.Status = StatusRunning
		
		// Log process start (SSE mode only)
		logMsg := fmt.Sprintf("🚀 Process started: %s", tracker.Command)
		if len(tracker.Args) > 0 {
			logMsg += fmt.Sprintf(" %v", tracker.Args)
		}
		logMsg += fmt.Sprintf(" (PID: %d, ID: %s", tracker.PID, tracker.ID)
		if tracker.Name != "" {
			logMsg += fmt.Sprintf(", name: %s", tracker.Name)
		}
		if tracker.SessionID != "" {
			logMsg += fmt.Sprintf(", session: %s", tracker.SessionID)
		}
		logMsg += ")"
		logIfNotTUI(logMsg)
		
		tracker.Mutex.Unlock()

		go streamToRingBuffer(stdoutPipe, tracker.StdoutBuffer)
		go streamToRingBuffer(stderrPipe, tracker.StderrBuffer)
	}

	go func() {
		err := cmd.Wait()
		tracker.Mutex.Lock()
		defer tracker.Mutex.Unlock()

		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode := exitError.ExitCode()
				tracker.ExitCode = &exitCode
				if exitCode != 0 {
					tracker.Status = StatusFailed
				} else {
					tracker.Status = StatusCompleted
				}
			} else {
				tracker.Status = StatusFailed
			}
		} else {
			exitCode := 0
			tracker.ExitCode = &exitCode
			tracker.Status = StatusCompleted
		}
		
		// Log process termination (SSE mode only)
		logMsg := fmt.Sprintf("💀 Process terminated: %s", tracker.Command)
		if tracker.Name != "" {
			logMsg += fmt.Sprintf(" (name: %s)", tracker.Name)
		}
		logMsg += fmt.Sprintf(" (PID: %d, ID: %s", tracker.PID, tracker.ID)
		if tracker.ExitCode != nil {
			logMsg += fmt.Sprintf(", exit code: %d", *tracker.ExitCode)
		}
		logMsg += fmt.Sprintf(", status: %s", tracker.Status)
		if tracker.SessionID != "" {
			logMsg += fmt.Sprintf(", session: %s", tracker.SessionID)
		}
		logMsg += ")"
		logIfNotTUI(logMsg)
	}()

	return nil
}

func handleSpawnProcess(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := request.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'command' argument"), nil
	}

	args := []string{}
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if argsInterface, exists := arguments["args"]; exists {
			if argsList, ok := argsInterface.([]any); ok {
				for _, arg := range argsList {
					if argStr, ok := arg.(string); ok {
						args = append(args, argStr)
					}
				}
			}
		}
	}

	workingDir := ""
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if wd, exists := arguments["working_dir"]; exists {
			if wdStr, ok := wd.(string); ok {
				workingDir = wdStr
			}
		}
	}

	envVars := map[string]string{}
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if env, exists := arguments["env"]; exists {
			if envMap, ok := env.(map[string]any); ok {
				for k, v := range envMap {
					if vStr, ok := v.(string); ok {
						envVars[k] = vStr
					}
				}
			}
		}
	}

	bufferSize := int64(DefaultBufferSize)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if bs, exists := arguments["buffer_size"]; exists {
			if bsFloat, ok := bs.(float64); ok {
				bufferSize = int64(bsFloat)
			}
		}
	}

	combineOutput := false
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if co, exists := arguments["combine_output"]; exists {
			if coBool, ok := co.(bool); ok {
				combineOutput = coBool
			}
		}
	}

	delay := time.Duration(0)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if d, exists := arguments["delay"]; exists {
			if dFloat, ok := d.(float64); ok {
				delayMs := int64(dFloat)
				if delayMs > MaxSpawnDelay {
					return mcp.NewToolResultError(fmt.Sprintf("Delay cannot exceed %d milliseconds (5 minutes)", MaxSpawnDelay)), nil
				}
				if delayMs < 0 {
					return mcp.NewToolResultError("Delay cannot be negative"), nil
				}
				delay = time.Duration(delayMs) * time.Millisecond
			}
		}
	}

	syncDelay := false
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if sd, exists := arguments["sync_delay"]; exists {
			if sdBool, ok := sd.(bool); ok {
				syncDelay = sdBool
			}
		}
	}

	name := ""
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if n, exists := arguments["name"]; exists {
			if nStr, ok := n.(string); ok {
				name = nStr
			}
		}
	}

	// Extract session ID from context (for SSE mode)
	sessionID := ExtractSessionFromContext(ctx)
	
	processID := uuid.New().String()
	tracker := &ProcessTracker{
		ID:            processID,
		Name:          name,
		SessionID:     sessionID,
		Command:       command,
		Args:          args,
		WorkingDir:    workingDir,
		BufferSize:    bufferSize,
		CombineOutput: combineOutput,
		DelayStart:    delay,
		SyncDelay:     syncDelay,
		StartTime:     time.Now(),
		LastAccessed:  time.Now(),
		Status:        StatusRunning, // Will be changed based on delay logic
		StdoutBuffer:  NewRingBuffer(bufferSize),
	}

	// Only create stderr buffer if not combining output
	if !combineOutput {
		tracker.StderrBuffer = NewRingBuffer(bufferSize)
	}

	// Handle delay logic
	var result map[string]any
	if delay > 0 {
		if syncDelay {
			// Sync mode: wait the delay, then execute and return actual status
			time.Sleep(delay)

			err := executeDelayedProcess(ctx, tracker, envVars)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			registry.addProcess(tracker)
			
			// Add to session manager if in SSE mode
			if sessionID != "" && sessionManager != nil {
				sessionManager.AddProcessToSession(sessionID, processID)
			}

			result = map[string]any{
				"process_id": processID,
				"pid":        tracker.PID,
				"status":     string(tracker.Status),
			}

		} else {
			// Async mode: set pending status, register immediately, start background delay
			tracker.Status = StatusPending
			registry.addProcess(tracker)
			
			// Add to session manager if in SSE mode
			if sessionID != "" && sessionManager != nil {
				sessionManager.AddProcessToSession(sessionID, processID)
			}

			// Start background goroutine to wait and then execute
			go func() {
				time.Sleep(delay)
				_ = executeDelayedProcess(context.Background(), tracker, envVars)
			}()

			result = map[string]any{
				"process_id": processID,
				"pid":        0, // No PID yet since process hasn't started
				"status":     string(tracker.Status),
			}
		}
	} else {
		// No delay: execute immediately (original behavior)
		err := executeDelayedProcess(ctx, tracker, envVars)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		registry.addProcess(tracker)
		
		// Add to session manager if in SSE mode
		if sessionID != "" && sessionManager != nil {
			sessionManager.AddProcessToSession(sessionID, processID)
		}

		result = map[string]any{
			"process_id": processID,
			"pid":        tracker.PID,
			"status":     string(tracker.Status),
		}
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func handleSpawnMultipleProcesses(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Parse the processes array
	var processes []map[string]any
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if procs, exists := arguments["processes"]; exists {
			if procsList, ok := procs.([]any); ok {
				for _, proc := range procsList {
					if procMap, ok := proc.(map[string]any); ok {
						processes = append(processes, procMap)
					}
				}
			}
		}
	}

	if len(processes) == 0 {
		return mcp.NewToolResultError("No processes specified"), nil
	}

	// Results to return
	results := []map[string]any{}

	// Deferred process info
	type processInfo struct {
		index     int
		tracker   *ProcessTracker
		envVars   map[string]string
		name      string
		processID string
	}

	var deferredProcesses []processInfo
	var deferredMode bool

	// Process each configuration
	for i, procConfig := range processes {
		// Extract configuration for this process
		command, exists := procConfig["command"].(string)
		if !exists {
			return mcp.NewToolResultError(fmt.Sprintf("Process %d missing required 'command' field", i)), nil
		}

		// Extract optional args
		args := []string{}
		if argsInterface, exists := procConfig["args"]; exists {
			if argsList, ok := argsInterface.([]any); ok {
				for _, arg := range argsList {
					if argStr, ok := arg.(string); ok {
						args = append(args, argStr)
					}
				}
			}
		}

		// Extract optional fields
		name, _ := procConfig["name"].(string)
		workingDir, _ := procConfig["working_dir"].(string)

		// Extract env vars
		envVars := map[string]string{}
		if env, exists := procConfig["env"]; exists {
			if envMap, ok := env.(map[string]any); ok {
				for k, v := range envMap {
					if vStr, ok := v.(string); ok {
						envVars[k] = vStr
					}
				}
			}
		}

		// Extract buffer size
		bufferSize := int64(DefaultBufferSize)
		if bs, exists := procConfig["buffer_size"]; exists {
			if bsFloat, ok := bs.(float64); ok {
				bufferSize = int64(bsFloat)
			}
		}

		// Extract combine output
		combineOutput := false
		if co, exists := procConfig["combine_output"]; exists {
			if coBool, ok := co.(bool); ok {
				combineOutput = coBool
			}
		}

		// Extract delay
		delay := time.Duration(0)
		if d, exists := procConfig["delay"]; exists {
			if dFloat, ok := d.(float64); ok {
				delayMs := int64(dFloat)
				if delayMs > MaxSpawnDelay {
					return mcp.NewToolResultError(fmt.Sprintf("Process %d: Delay cannot exceed %d milliseconds (5 minutes)", i, MaxSpawnDelay)), nil
				}
				if delayMs < 0 {
					return mcp.NewToolResultError(fmt.Sprintf("Process %d: Delay cannot be negative", i)), nil
				}
				delay = time.Duration(delayMs) * time.Millisecond
			}
		}

		// Extract sync_delay
		syncDelay := false
		if sd, exists := procConfig["sync_delay"]; exists {
			if sdBool, ok := sd.(bool); ok {
				syncDelay = sdBool
			}
		}

		// Create tracker
		processID := uuid.New().String()
		
		// Extract session ID from context (for SSE mode)
		sessionID := ExtractSessionFromContext(ctx)
		
		tracker := &ProcessTracker{
			ID:            processID,
			Name:          name,
			SessionID:     sessionID,
			Command:       command,
			Args:          args,
			WorkingDir:    workingDir,
			BufferSize:    bufferSize,
			CombineOutput: combineOutput,
			DelayStart:    delay,
			SyncDelay:     syncDelay,
			StartTime:     time.Now(),
			LastAccessed:  time.Now(),
			Status:        StatusRunning,
			StdoutBuffer:  NewRingBuffer(bufferSize),
		}

		if !combineOutput {
			tracker.StderrBuffer = NewRingBuffer(bufferSize)
		}

		// Determine if we need to defer this process
		shouldDefer := deferredMode || (!syncDelay && (delay > 0 || deferredMode))

		if shouldDefer {
			// We're in deferred mode - add to deferred list
			deferredMode = true
			tracker.Status = StatusPending
			registry.addProcess(tracker)
			
			// Add to session manager if in SSE mode
			if sessionID != "" && sessionManager != nil {
				sessionManager.AddProcessToSession(sessionID, processID)
			}

			deferredProcesses = append(deferredProcesses, processInfo{
				index:     i,
				tracker:   tracker,
				envVars:   envVars,
				name:      name,
				processID: processID,
			})

			results = append(results, map[string]any{
				"index":      i,
				"name":       name,
				"process_id": processID,
				"pid":        0,
				"status":     "pending",
			})
		} else {
			// Process immediately (sync mode or no delay in non-deferred mode)
			if delay > 0 {
				// Wait for the delay
				time.Sleep(delay)
			}

			err := executeDelayedProcess(ctx, tracker, envVars)
			if err != nil {
				results = append(results, map[string]any{
					"index":      i,
					"name":       name,
					"process_id": processID,
					"error":      err.Error(),
				})
				continue
			}

			registry.addProcess(tracker)
			
			// Add to session manager if in SSE mode
			if sessionID != "" && sessionManager != nil {
				sessionManager.AddProcessToSession(sessionID, processID)
			}

			results = append(results, map[string]any{
				"index":      i,
				"name":       name,
				"process_id": processID,
				"pid":        tracker.PID,
				"status":     string(tracker.Status),
			})
		}
	}

	// If we have deferred processes, start them in a goroutine
	if len(deferredProcesses) > 0 {
		go func() {
			// Process each deferred process with its delay
			for i, info := range deferredProcesses {
				// For the first deferred process, use its original delay
				// For subsequent ones, use their individual delays
				if i == 0 {
					// First deferred process - wait for its delay
					if info.tracker.DelayStart > 0 {
						time.Sleep(info.tracker.DelayStart)
					}
				} else {
					// Subsequent processes - wait for their individual delays
					// This is the delay between this process and the previous one
					if info.tracker.DelayStart > 0 {
						time.Sleep(info.tracker.DelayStart)
					}
				}

				// Execute the process
				err := executeDelayedProcess(context.Background(), info.tracker, info.envVars)
				if err != nil {
					// Process failed to start - update status
					info.tracker.Mutex.Lock()
					info.tracker.Status = StatusFailed
					info.tracker.Mutex.Unlock()
				}
			}
		}()
	}

	resultBytes, _ := json.Marshal(results)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func streamToRingBuffer(reader io.ReadCloser, buffer *RingBuffer) {
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		buffer.Write([]byte(line))
	}
}

func handleGetPartialProcessOutput(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processID, err := request.RequireString("process_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'process_id' argument"), nil
	}

	streams := "both"
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if s, exists := arguments["streams"]; exists {
			if sStr, ok := s.(string); ok {
				streams = sStr
			}
		}
	}

	maxLines := -1
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if ml, exists := arguments["max_lines"]; exists {
			if mlFloat, ok := ml.(float64); ok {
				maxLines = int(mlFloat)
			}
		}
	}

	// Parse filters parameter
	var filters [][]string
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if f, exists := arguments["filters"]; exists {
			if filtersArray, ok := f.([]any); ok {
				for _, filterInterface := range filtersArray {
					if filterCmd, ok := filterInterface.([]any); ok {
						var cmd []string
						for _, arg := range filterCmd {
							if argStr, ok := arg.(string); ok {
								cmd = append(cmd, argStr)
							}
						}
						if len(cmd) > 0 {
							filters = append(filters, cmd)
						}
					}
				}
			}
		}
	}

	// Parse delay parameter
	delay := time.Duration(0)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if d, exists := arguments["delay"]; exists {
			if dFloat, ok := d.(float64); ok {
				delayMs := int64(dFloat)
				if delayMs > MaxOutputDelay {
					return mcp.NewToolResultError(fmt.Sprintf("Delay cannot exceed %d milliseconds (2 minutes)", MaxOutputDelay)), nil
				}
				if delayMs < 0 {
					return mcp.NewToolResultError("Delay cannot be negative"), nil
				}
				delay = time.Duration(delayMs) * time.Millisecond
			}
		}
	}

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	// Implement smart delay logic
	if delay > 0 {
		ticker := time.NewTicker(time.Duration(DelayCheckInterval) * time.Millisecond)
		defer ticker.Stop()

		remaining := delay
	delayLoop:
		for remaining > 0 {
			select {
			case <-ticker.C:
				// Check if process has terminated
				tracker.Mutex.RLock()
				status := tracker.Status
				tracker.Mutex.RUnlock()

				if status != StatusRunning && status != StatusPending {
					// Process terminated, exit delay loop
					break delayLoop
				}
				remaining -= time.Duration(DelayCheckInterval) * time.Millisecond

			case <-ctx.Done():
				// Context canceled
				return mcp.NewToolResultError("Request canceled"), nil
			}
		}
	}

	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()

	response := &OutputResponse{
		ProcessID:    processID,
		StdoutCursor: tracker.StdoutCursor,
		StderrCursor: tracker.StderrCursor,
		Status:       tracker.Status,
		ExitCode:     tracker.ExitCode,
	}

	if tracker.CombineOutput {
		// When output is combined, everything is in StdoutBuffer
		if streams == "stderr" {
			// Special case: user wants stderr but output is combined
			return mcp.NewToolResultError("Process has combined output - stderr not available separately. Use 'stdout' or 'both' streams."), nil
		}

		// Get combined output from StdoutBuffer
		stdout := extractNewContentFromRingBuffer(tracker.StdoutBuffer, tracker.StdoutCursor, maxLines)

		// Apply filters if provided
		if len(filters) > 0 {
			filteredOutput, filterErr := filterOutput(stdout, filters)
			if filterErr != nil {
				// Return warning but still include original output
				response.Stdout = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, stdout)
			} else {
				response.Stdout = filteredOutput
			}
		} else {
			response.Stdout = stdout
		}

		response.StdoutCursor = tracker.StdoutBuffer.TotalBytes()
		tracker.StdoutCursor = response.StdoutCursor
	} else {
		// Separate output streams (original behavior)
		if streams == "stdout" || streams == "both" {
			stdout := extractNewContentFromRingBuffer(tracker.StdoutBuffer, tracker.StdoutCursor, maxLines)

			// Apply filters to stdout if provided
			if len(filters) > 0 {
				filteredOutput, filterErr := filterOutput(stdout, filters)
				if filterErr != nil {
					response.Stdout = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, stdout)
				} else {
					response.Stdout = filteredOutput
				}
			} else {
				response.Stdout = stdout
			}

			response.StdoutCursor = tracker.StdoutBuffer.TotalBytes()
			tracker.StdoutCursor = response.StdoutCursor
		}

		if streams == "stderr" || streams == "both" {
			stderr := extractNewContentFromRingBuffer(tracker.StderrBuffer, tracker.StderrCursor, maxLines)

			// Apply filters to stderr if provided
			if len(filters) > 0 {
				filteredOutput, filterErr := filterOutput(stderr, filters)
				if filterErr != nil {
					response.Stderr = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, stderr)
				} else {
					response.Stderr = filteredOutput
				}
			} else {
				response.Stderr = stderr
			}

			response.StderrCursor = tracker.StderrBuffer.TotalBytes()
			tracker.StderrCursor = response.StderrCursor
		}
	}

	resultBytes, _ := json.Marshal(response)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func extractNewContentFromRingBuffer(buffer *RingBuffer, cursor int64, maxLines int) string {
	newContent := buffer.GetContentFromCursor(cursor)
	if maxLines > 0 && newContent != "" {
		lines := strings.Split(newContent, "\n")
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			newContent = strings.Join(lines, "\n")
			if !strings.HasSuffix(newContent, "\n") && len(lines) > 0 {
				newContent += "\n"
			}
		}
	}

	return newContent
}

func handleGetFullProcessOutput(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processID, err := request.RequireString("process_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'process_id' argument"), nil
	}

	streams := "both"
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if s, exists := arguments["streams"]; exists {
			if sStr, ok := s.(string); ok {
				streams = sStr
			}
		}
	}

	maxLines := -1
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if ml, exists := arguments["max_lines"]; exists {
			if mlFloat, ok := ml.(float64); ok {
				maxLines = int(mlFloat)
			}
		}
	}

	// Parse filters parameter
	var filters [][]string
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if f, exists := arguments["filters"]; exists {
			if filtersArray, ok := f.([]any); ok {
				for _, filterInterface := range filtersArray {
					if filterCmd, ok := filterInterface.([]any); ok {
						var cmd []string
						for _, arg := range filterCmd {
							if argStr, ok := arg.(string); ok {
								cmd = append(cmd, argStr)
							}
						}
						if len(cmd) > 0 {
							filters = append(filters, cmd)
						}
					}
				}
			}
		}
	}

	// Parse delay parameter
	delay := time.Duration(0)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if d, exists := arguments["delay"]; exists {
			if dFloat, ok := d.(float64); ok {
				delayMs := int64(dFloat)
				if delayMs > MaxOutputDelay {
					return mcp.NewToolResultError(fmt.Sprintf("Delay cannot exceed %d milliseconds (2 minutes)", MaxOutputDelay)), nil
				}
				if delayMs < 0 {
					return mcp.NewToolResultError("Delay cannot be negative"), nil
				}
				delay = time.Duration(delayMs) * time.Millisecond
			}
		}
	}

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	// Implement smart delay logic
	if delay > 0 {
		ticker := time.NewTicker(time.Duration(DelayCheckInterval) * time.Millisecond)
		defer ticker.Stop()

		remaining := delay
	delayLoop:
		for remaining > 0 {
			select {
			case <-ticker.C:
				// Check if process has terminated
				tracker.Mutex.RLock()
				status := tracker.Status
				tracker.Mutex.RUnlock()

				if status != StatusRunning && status != StatusPending {
					// Process terminated, exit delay loop
					break delayLoop
				}
				remaining -= time.Duration(DelayCheckInterval) * time.Millisecond

			case <-ctx.Done():
				// Context canceled
				return mcp.NewToolResultError("Request canceled"), nil
			}
		}
	}

	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()

	// Handle cursor values properly for combined vs separate output
	var stdoutCursor, stderrCursor int64
	if tracker.CombineOutput {
		stdoutCursor = tracker.StdoutBuffer.TotalBytes()
		stderrCursor = 0 // Not used when combined
	} else {
		stdoutCursor = tracker.StdoutBuffer.TotalBytes()
		if tracker.StderrBuffer != nil {
			stderrCursor = tracker.StderrBuffer.TotalBytes()
		}
	}

	response := &OutputResponse{
		ProcessID:    processID,
		StdoutCursor: stdoutCursor,
		StderrCursor: stderrCursor,
		Status:       tracker.Status,
		ExitCode:     tracker.ExitCode,
	}

	if tracker.CombineOutput {
		// When output is combined, everything is in StdoutBuffer
		if streams == "stderr" {
			// Special case: user wants stderr but output is combined
			return mcp.NewToolResultError("Process has combined output - stderr not available separately. Use 'stdout' or 'both' streams."), nil
		}

		// Get combined output from StdoutBuffer
		fullStdout := tracker.StdoutBuffer.GetContent()
		if maxLines > 0 && fullStdout != "" {
			lines := strings.Split(fullStdout, "\n")
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				fullStdout = strings.Join(lines, "\n")
				if !strings.HasSuffix(fullStdout, "\n") && len(lines) > 0 {
					fullStdout += "\n"
				}
			}
		}

		// Apply filters if provided
		if len(filters) > 0 {
			filteredOutput, filterErr := filterOutput(fullStdout, filters)
			if filterErr != nil {
				// Return warning but still include original output
				response.Stdout = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, fullStdout)
			} else {
				response.Stdout = filteredOutput
			}
		} else {
			response.Stdout = fullStdout
		}
	} else {
		// Separate output streams (original behavior)
		if streams == "stdout" || streams == "both" {
			fullStdout := tracker.StdoutBuffer.GetContent()
			if maxLines > 0 && fullStdout != "" {
				lines := strings.Split(fullStdout, "\n")
				if len(lines) > maxLines {
					lines = lines[:maxLines]
					fullStdout = strings.Join(lines, "\n")
					if !strings.HasSuffix(fullStdout, "\n") && len(lines) > 0 {
						fullStdout += "\n"
					}
				}
			}

			// Apply filters to stdout if provided
			if len(filters) > 0 {
				filteredOutput, filterErr := filterOutput(fullStdout, filters)
				if filterErr != nil {
					response.Stdout = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, fullStdout)
				} else {
					response.Stdout = filteredOutput
				}
			} else {
				response.Stdout = fullStdout
			}
		}

		if streams == "stderr" || streams == "both" {
			fullStderr := tracker.StderrBuffer.GetContent()
			if maxLines > 0 && fullStderr != "" {
				lines := strings.Split(fullStderr, "\n")
				if len(lines) > maxLines {
					lines = lines[:maxLines]
					fullStderr = strings.Join(lines, "\n")
					if !strings.HasSuffix(fullStderr, "\n") && len(lines) > 0 {
						fullStderr += "\n"
					}
				}
			}

			// Apply filters to stderr if provided
			if len(filters) > 0 {
				filteredOutput, filterErr := filterOutput(fullStderr, filters)
				if filterErr != nil {
					response.Stderr = fmt.Sprintf("FILTER WARNING: %v\n\n%s", filterErr, fullStderr)
				} else {
					response.Stderr = filteredOutput
				}
			} else {
				response.Stderr = fullStderr
			}
		}
	}

	resultBytes, _ := json.Marshal(response)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func handleSendProcessInput(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processID, err := request.RequireString("process_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'process_id' argument"), nil
	}

	input, err := request.RequireString("input")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'input' argument"), nil
	}

	// Extract auto_newline parameter (defaults to true)
	autoNewline := true
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if an, exists := arguments["auto_newline"]; exists {
			if anBool, ok := an.(bool); ok {
				autoNewline = anBool
			}
		}
	}

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()

	if tracker.Status != StatusRunning {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s is not running (status: %s)", processID, tracker.Status)), nil
	}

	if tracker.StdinWriter == nil {
		return mcp.NewToolResultError("Process stdin is not available"), nil
	}

	// Prepare the final input to send
	finalInput := input
	if autoNewline {
		finalInput = input + "\n"
	}

	_, err = tracker.StdinWriter.Write([]byte(finalInput))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write to process stdin: %v", err)), nil
	}

	// Prepare result message
	message := fmt.Sprintf("Sent %d bytes to process stdin", len(finalInput))
	if autoNewline {
		message += " (with newline)"
	}

	result := map[string]any{
		"process_id":    processID,
		"status":        "input_sent",
		"message":       message,
		"auto_newline":  autoNewline,
		"bytes_sent":    len(finalInput),
		"original_size": len(input),
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func handleListProcesses(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processes := registry.getAllProcesses()

	result := make([]map[string]any, 0, len(processes))
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		processInfo := map[string]any{
			"id":             tracker.ID,
			"name":           tracker.Name,
			"pid":            tracker.PID,
			"command":        tracker.Command,
			"args":           tracker.Args,
			"working_dir":    tracker.WorkingDir,
			"buffer_size":    tracker.BufferSize,
			"combine_output": tracker.CombineOutput,
			"delay_start":    int64(tracker.DelayStart / time.Millisecond),
			"sync_delay":     tracker.SyncDelay,
			"start_time":     tracker.StartTime.Format(time.RFC3339),
			"last_accessed":  tracker.LastAccessed.Format(time.RFC3339),
			"status":         string(tracker.Status),
		}
		if tracker.ExitCode != nil {
			processInfo["exit_code"] = *tracker.ExitCode
		}
		tracker.Mutex.RUnlock()
		result = append(result, processInfo)
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func handleKillProcess(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processID, err := request.RequireString("process_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'process_id' argument"), nil
	}

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()

	if tracker.Status != StatusRunning {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s is not running (status: %s)", processID, tracker.Status)), nil
	}

	if tracker.Process != nil && tracker.Process.Process != nil {
		// Close stdin first to signal the process
		if tracker.StdinWriter != nil {
			tracker.StdinWriter.Close()
		}

		// Kill the entire process group (Unix) or process (Windows)
		err := terminateProcessGroup(tracker.Process.Process.Pid)
		if err != nil {
			// If platform-specific termination fails, use standard process.Kill()
			if tracker.Process.Process != nil {
				tracker.Process.Process.Kill()
			}
		}
		tracker.Status = StatusKilled
		
		// Log manual kill (SSE mode only)
			logMsg := fmt.Sprintf("🔫 Process killed manually: %s", tracker.Command)
			if tracker.Name != "" {
				logMsg += fmt.Sprintf(" (name: %s)", tracker.Name)
			}
			logMsg += fmt.Sprintf(" (PID: %d, ID: %s", tracker.PID, processID)
			if tracker.SessionID != "" {
				logMsg += fmt.Sprintf(", session: %s", tracker.SessionID)
			}
			logMsg += ")"
			logIfNotTUI(logMsg)
	}

	result := map[string]any{
		"process_id": processID,
		"status":     string(tracker.Status),
		"message":    "Process terminated",
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func handleGetProcessStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	processID, err := request.RequireString("process_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'process_id' argument"), nil
	}

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	tracker.Mutex.RLock()
	defer tracker.Mutex.RUnlock()

	result := map[string]any{
		"id":             tracker.ID,
		"name":           tracker.Name,
		"pid":            tracker.PID,
		"command":        tracker.Command,
		"args":           tracker.Args,
		"working_dir":    tracker.WorkingDir,
		"buffer_size":    tracker.BufferSize,
		"combine_output": tracker.CombineOutput,
		"delay_start":    int64(tracker.DelayStart / time.Millisecond),
		"sync_delay":     tracker.SyncDelay,
		"start_time":     tracker.StartTime.Format(time.RFC3339),
		"last_accessed":  tracker.LastAccessed.Format(time.RFC3339),
		"status":         string(tracker.Status),
		"stdout_cursor":  tracker.StdoutCursor,
		"stderr_cursor":  tracker.StderrCursor,
		"stdout_size":    tracker.StdoutBuffer.Len(),
		"stdout_total":   tracker.StdoutBuffer.TotalBytes(),
	}

	if tracker.CombineOutput {
		// When output is combined, stderr info is not relevant
		result["stderr_size"] = 0
		result["stderr_total"] = 0
		result["combined_output_note"] = "stdout contains both stdout and stderr (combined)"
	} else {
		// Separate streams - include stderr info
		if tracker.StderrBuffer != nil {
			result["stderr_size"] = tracker.StderrBuffer.Len()
			result["stderr_total"] = tracker.StderrBuffer.TotalBytes()
		} else {
			result["stderr_size"] = 0
			result["stderr_total"] = 0
		}
	}

	if tracker.ExitCode != nil {
		result["exit_code"] = *tracker.ExitCode
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}
