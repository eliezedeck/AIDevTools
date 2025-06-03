package main

import (
	"bufio"
	"bytes"
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
	StartTime     time.Time       `json:"start_time"`
	LastAccessed  time.Time       `json:"last_accessed"`
	Status        ProcessStatus   `json:"status"`
	StdoutCursor  int64           `json:"stdout_cursor"`
	StderrCursor  int64           `json:"stderr_cursor"`
	StdoutBuffer  *bytes.Buffer   `json:"-"`
	StderrBuffer  *bytes.Buffer   `json:"-"`
	Process       *exec.Cmd       `json:"-"`
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

	processID := uuid.New().String()
	tracker := &ProcessTracker{
		ID:           processID,
		Command:      command,
		Args:         args,
		WorkingDir:   workingDir,
		StartTime:    time.Now(),
		LastAccessed: time.Now(),
		Status:       StatusRunning,
		StdoutBuffer: &bytes.Buffer{},
		StderrBuffer: &bytes.Buffer{},
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

	if err := cmd.Start(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to start process: %v", err)), nil
	}

	tracker.Process = cmd
	tracker.PID = cmd.Process.Pid

	go streamToBuffer(stdoutPipe, tracker.StdoutBuffer, &tracker.Mutex)
	go streamToBuffer(stderrPipe, tracker.StderrBuffer, &tracker.Mutex)

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

func streamToBuffer(reader io.ReadCloser, buffer *bytes.Buffer, mutex *sync.RWMutex) {
	defer reader.Close()
	
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		mutex.Lock()
		buffer.WriteString(line)
		mutex.Unlock()
	}
}

func handleGetProcessOutput(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		stdout := extractNewContent(tracker.StdoutBuffer, tracker.StdoutCursor, maxLines)
		response.Stdout = stdout
		response.StdoutCursor = int64(tracker.StdoutBuffer.Len())
		tracker.StdoutCursor = response.StdoutCursor
	}

	if streams == "stderr" || streams == "both" {
		stderr := extractNewContent(tracker.StderrBuffer, tracker.StderrCursor, maxLines)
		response.Stderr = stderr
		response.StderrCursor = int64(tracker.StderrBuffer.Len())
		tracker.StderrCursor = response.StderrCursor
	}

	resultBytes, _ := json.Marshal(response)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

func extractNewContent(buffer *bytes.Buffer, cursor int64, maxLines int) string {
	content := buffer.String()
	if cursor >= int64(len(content)) {
		return ""
	}

	newContent := content[cursor:]
	if maxLines > 0 {
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
		"start_time":    tracker.StartTime.Format(time.RFC3339),
		"last_accessed": tracker.LastAccessed.Format(time.RFC3339),
		"status":        string(tracker.Status),
		"stdout_cursor": tracker.StdoutCursor,
		"stderr_cursor": tracker.StderrCursor,
		"stdout_size":   tracker.StdoutBuffer.Len(),
		"stderr_size":   tracker.StderrBuffer.Len(),
	}

	if tracker.ExitCode != nil {
		result["exit_code"] = *tracker.ExitCode
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}