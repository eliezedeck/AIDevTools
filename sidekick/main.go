package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// 🛠️ Create a new MCP server
	s := server.NewMCPServer(
		"Sidekick Notifications",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// 🗣️ Define the notifications_speak tool
	speakTool := mcp.NewTool(
		"notifications_speak",
		mcp.WithDescription("Play a system sound and speak the provided text (max 50 words)"),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Text to speak (max 50 words)"),
		),
	)

	// 🔗 Register the tool handler
	s.AddTool(speakTool, handleSpeak)

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
			mcp.Description("Delay before starting process in milliseconds (max: 300000 = 5 minutes)"),
		),
		mcp.WithBoolean("sync_delay",
			mcp.Description("Whether delay blocks caller (true) or returns immediately (false, default)"),
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
			mcp.Description("Delay before returning output in milliseconds (max: 120000 = 2 minutes)"),
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
			mcp.Description("Delay before returning output in milliseconds (max: 120000 = 2 minutes)"),
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

	// 🔗 Register process management tools
	s.AddTool(spawnProcessTool, handleSpawnProcess)
	s.AddTool(getPartialProcessOutputTool, handleGetPartialProcessOutput)
	s.AddTool(getFullProcessOutputTool, handleGetFullProcessOutput)
	s.AddTool(sendProcessInputTool, handleSendProcessInput)
	s.AddTool(listProcessesTool, handleListProcesses)
	s.AddTool(killProcessTool, handleKillProcess)
	s.AddTool(getProcessStatusTool, handleGetProcessStatus)

	// 🚦 Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	// Handle signals in a goroutine
	go func() {
		<-sigChan
		handleGracefulShutdown()
		os.Exit(0)
	}()

	// 🚦 Start the MCP server over stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

// handleGracefulShutdown sends SIGTERM to all running processes, waits up to 5 seconds,
// then sends SIGKILL to any remaining processes
func handleGracefulShutdown() {
	// Get all tracked processes
	processes := registry.getAllProcesses()
	
	// Send SIGTERM to all running process groups
	for _, tracker := range processes {
		tracker.Mutex.RLock()
		if tracker.Process != nil && tracker.Process.Process != nil && 
		   (tracker.Status == StatusRunning || tracker.Status == StatusPending) {
			// Kill the entire process group by sending signal to -pid
			_ = syscall.Kill(-tracker.Process.Process.Pid, syscall.SIGTERM)
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
			// Kill the entire process group with SIGKILL
			_ = syscall.Kill(-tracker.Process.Process.Pid, syscall.SIGKILL)
		}
		tracker.Mutex.RUnlock()
	}
}
