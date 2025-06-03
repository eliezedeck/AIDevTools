package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SpeakParams defines the input for the notifications_speak tool 🚀
type SpeakParams struct {
	Text string `json:"text" mcp:"Text to speak (max 50 words)"`
}

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

// handleSpeak executes the notifications_speak tool logic 🎤
func handleSpeak(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := request.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'text' argument"), nil
	}

	words := strings.Fields(text)
	if len(words) > 50 {
		return mcp.NewToolResultError("Text must be 50 words or less"), nil
	}

	// 🔊 Play system sound asynchronously
	go func() {
		exec.Command("afplay", "/System/Library/Sounds/Glass.aiff", "-v", "10").Run()
	}()

	// 🗣️ Speak the text after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		exec.Command("say", "-v", "Zoe (Premium)", text).Run()
	}()

	return mcp.NewToolResultText("Notification spoken!"), nil
}
