package main

import (
	"fmt"

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
		mcp.WithDescription("Spawn a new process and start tracking its output"),
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
	)

	getProcessOutputTool := mcp.NewTool(
		"get_process_output",
		mcp.WithDescription("Get incremental output from a process since last read"),
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
	s.AddTool(getProcessOutputTool, handleGetProcessOutput)
	s.AddTool(listProcessesTool, handleListProcesses)
	s.AddTool(killProcessTool, handleKillProcess)
	s.AddTool(getProcessStatusTool, handleGetProcessStatus)

	// 🚦 Start the MCP server over stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
