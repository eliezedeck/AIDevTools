package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// SpeakParams defines the input for the notifications_speak tool 🚀
type SpeakParams struct {
	Text string `json:"text" mcp:"Text to speak (max 50 words)"`
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
		_ = exec.Command("afplay", "/System/Library/Sounds/Glass.aiff", "-v", "5").Run()
	}()

	// 🗣️ Speak the text after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = exec.Command("say", "-v", "Zoe (Premium)", text).Run()
	}()

	return mcp.NewToolResultText("Notification spoken!"), nil
}
