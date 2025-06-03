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
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

type ProcessStatus string

const (
	StatusRunning   ProcessStatus = "running"
	StatusCompleted ProcessStatus = "completed"
	StatusFailed    ProcessStatus = "failed"
	StatusKilled    ProcessStatus = "killed"
)

type ProcessTracker struct {
	ID            string          `json:"id"`
	PID           int             `json:"pid"`
	Command       string          `json:"command"`
	Args          []string        `json:"args"`
	WorkingDir    string          `json:"working_dir"`
	BufferSize    int64           `json:"buffer_size"`
	StartTime     time.Time       `json:"start_time"`
	LastAccessed  time.Time       `json:"last_accessed"`
	Status        ProcessStatus   `json:"status"`
	StdoutCursor  int64           `json:"stdout_cursor"`
	StderrCursor  int64           `json:"stderr_cursor"`
	StdoutBuffer  *RingBuffer     `json:"-"`
	StderrBuffer  *RingBuffer     `json:"-"`
	Process       *exec.Cmd       `json:"-"`
	StdinWriter   io.WriteCloser  `json:"-"`
	ExitCode      *int            `json:"exit_code,omitempty"`
	Mutex         sync.RWMutex    `json:"-"`
}

type OutputResponse struct {
	ProcessID     string          `json:"process_id"`
	Stdout        string          `json:"stdout,omitempty"`
	Stderr        string          `json:"stderr,omitempty"`
	StdoutCursor  int64           `json:"stdout_cursor"`
	StderrCursor  int64           `json:"stderr_cursor"`
	Status        ProcessStatus   `json:"status"`
	ExitCode      *int            `json:"exit_code,omitempty"`
}

type ProcessRegistry struct {
	processes map[string]*ProcessTracker
	mutex     sync.RWMutex
}

const (
	DefaultBufferSize = 10 * 1024 * 1024 // 10MB default buffer size
)

type RingBuffer struct {
	data        []byte
	maxSize     int64
	totalBytes  int64
	mutex       sync.RWMutex
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
	
	if effectivePos < 0 {
		effectivePos = 0
	}
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

	processID := uuid.New().String()
	tracker := &ProcessTracker{
		ID:           processID,
		Command:      command,
		Args:         args,
		WorkingDir:   workingDir,
		BufferSize:   bufferSize,
		StartTime:    time.Now(),
		LastAccessed: time.Now(),
		Status:       StatusRunning,
		StdoutBuffer: NewRingBuffer(bufferSize),
		StderrBuffer: NewRingBuffer(bufferSize),
	}

	cmd := exec.CommandContext(ctx, command, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	env := os.Environ()
	env = append(env, "NO_COLOR=1", "TERM=dumb")
	for k, v := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create stdout pipe: %v", err)), nil
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create stderr pipe: %v", err)), nil
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create stdin pipe: %v", err)), nil
	}

	if err := cmd.Start(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to start process: %v", err)), nil
	}

	tracker.Process = cmd
	tracker.PID = cmd.Process.Pid
	tracker.StdinWriter = stdinPipe

	go streamToRingBuffer(stdoutPipe, tracker.StdoutBuffer)
	go streamToRingBuffer(stderrPipe, tracker.StderrBuffer)

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
	}()

	registry.addProcess(tracker)

	result := map[string]any{
		"process_id": processID,
		"pid":        tracker.PID,
		"status":     string(tracker.Status),
	}

	resultBytes, _ := json.Marshal(result)
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

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
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

	if streams == "stdout" || streams == "both" {
		stdout := extractNewContentFromRingBuffer(tracker.StdoutBuffer, tracker.StdoutCursor, maxLines)
		response.Stdout = stdout
		response.StdoutCursor = tracker.StdoutBuffer.TotalBytes()
		tracker.StdoutCursor = response.StdoutCursor
	}

	if streams == "stderr" || streams == "both" {
		stderr := extractNewContentFromRingBuffer(tracker.StderrBuffer, tracker.StderrCursor, maxLines)
		response.Stderr = stderr
		response.StderrCursor = tracker.StderrBuffer.TotalBytes()
		tracker.StderrCursor = response.StderrCursor
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

	tracker, exists := registry.getProcess(processID)
	if !exists {
		return mcp.NewToolResultError(fmt.Sprintf("Process %s not found", processID)), nil
	}

	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()

	response := &OutputResponse{
		ProcessID:    processID,
		StdoutCursor: tracker.StdoutBuffer.TotalBytes(),
		StderrCursor: tracker.StderrBuffer.TotalBytes(),
		Status:       tracker.Status,
		ExitCode:     tracker.ExitCode,
	}

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
		response.Stdout = fullStdout
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
		response.Stderr = fullStderr
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

	_, err = tracker.StdinWriter.Write([]byte(input))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write to process stdin: %v", err)), nil
	}

	result := map[string]any{
		"process_id": processID,
		"status":     "input_sent",
		"message":    fmt.Sprintf("Sent %d bytes to process stdin", len(input)),
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
			"id":            tracker.ID,
			"pid":           tracker.PID,
			"command":       tracker.Command,
			"args":          tracker.Args,
			"working_dir":   tracker.WorkingDir,
			"start_time":    tracker.StartTime.Format(time.RFC3339),
			"last_accessed": tracker.LastAccessed.Format(time.RFC3339),
			"status":        string(tracker.Status),
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
		
		err := tracker.Process.Process.Signal(syscall.SIGTERM)
		if err != nil {
			tracker.Process.Process.Kill()
		}
		tracker.Status = StatusKilled
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
		"id":            tracker.ID,
		"pid":           tracker.PID,
		"command":       tracker.Command,
		"args":          tracker.Args,
		"working_dir":   tracker.WorkingDir,
		"buffer_size":   tracker.BufferSize,
		"start_time":    tracker.StartTime.Format(time.RFC3339),
		"last_accessed": tracker.LastAccessed.Format(time.RFC3339),
		"status":        string(tracker.Status),
		"stdout_cursor": tracker.StdoutCursor,
		"stderr_cursor": tracker.StderrCursor,
		"stdout_size":   tracker.StdoutBuffer.Len(),
		"stderr_size":   tracker.StderrBuffer.Len(),
		"stdout_total":  tracker.StdoutBuffer.TotalBytes(),
		"stderr_total":  tracker.StderrBuffer.TotalBytes(),
	}

	if tracker.ExitCode != nil {
		result["exit_code"] = *tracker.ExitCode
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}