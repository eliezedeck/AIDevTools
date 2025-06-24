package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// handleAnswerQuestion provides an answer to a question. A question can only be answered once and only once.
func handleAnswerQuestion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get required parameters
	questionID, err := request.RequireString("question_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'question_id' argument"), nil
	}

	answer, err := request.RequireString("answer")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'answer' argument"), nil
	}

	// Submit the answer
	err = agentQARegistry.AnswerQuestion(questionID, answer, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"status":      "answer_submitted",
		"question_id": questionID,
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

// handleGetNextQuestion waits for and retrieves the next question for this specialist
func handleGetNextQuestion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get required parameters
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'name' argument"), nil
	}

	specialty, err := request.RequireString("specialty")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'specialty' argument"), nil
	}

	rootDir, err := request.RequireString("root_dir")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'root_dir' argument"), nil
	}

	// Get optional instructions
	instructions := ""
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if inst, exists := arguments["instructions"]; exists {
			if instStr, ok := inst.(string); ok {
				instructions = instStr
			}
		}
	}

	// Get wait parameter (default: true)
	wait := true
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if w, exists := arguments["wait"]; exists {
			if wBool, ok := w.(bool); ok {
				wait = wBool
			}
		}
	}

	// Get timeout parameter (default: 0 = no timeout)
	timeout := time.Duration(0)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if t, exists := arguments["timeout"]; exists {
			if tFloat, ok := t.(float64); ok {
				timeoutMs := int64(tFloat)
				if timeoutMs > 0 {
					timeout = time.Duration(timeoutMs) * time.Millisecond
				}
			}
		}
	}

	// If not waiting, check if there's a question immediately available
	if !wait {
		// Try to get a question without blocking
		qa, err := agentQARegistry.WaitForQuestionWithContext(ctx, name, specialty, rootDir, instructions, 1*time.Millisecond)
		if err != nil {
			return mcp.NewToolResultError("No questions available"), nil
		}

		result := map[string]any{
			"question_id": qa.ID,
			"from":        qa.From,
			"question":    qa.Question,
			"timestamp":   qa.Timestamp.Format(time.RFC3339),
		}

		resultBytes, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultBytes)), nil
	}

	// Wait for next question with context cancellation support
	LogInfo("AgentQA", "Waiting for next question", fmt.Sprintf("Name: %s, Specialty: %s, RootDir: %s, Timeout: %v", name, specialty, rootDir, timeout))

	qa, err := agentQARegistry.WaitForQuestionWithContext(ctx, name, specialty, rootDir, instructions, timeout)
	if err != nil {
		LogError("AgentQA", "Error waiting for question", fmt.Sprintf("Specialty: %s, Error: %v", specialty, err))

		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			LogInfo("AgentQA", "Request cancelled by context", fmt.Sprintf("Context error: %v", ctx.Err()))
			return mcp.NewToolResultError("Request cancelled"), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	LogInfo("AgentQA", "Question received", fmt.Sprintf("QuestionID: %s, From: %s", qa.ID, qa.From))

	result := map[string]any{
		"question_id": qa.ID,
		"from":        qa.From,
		"question":    qa.Question,
		"timestamp":   qa.Timestamp.Format(time.RFC3339),
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		LogError("AgentQA", "Failed to marshal response", fmt.Sprintf("Error: %v", err))
		return mcp.NewToolResultError("Failed to marshal response"), nil
	}

	LogInfo("AgentQA", "Returning question response", fmt.Sprintf("ResponseSize: %d bytes", len(resultBytes)))
	return mcp.NewToolResultText(string(resultBytes)), nil
}

// handleAskSpecialist asks a question to a specialist
func handleAskSpecialist(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	specialty, err := request.RequireString("specialty")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'specialty' argument"), nil
	}

	rootDir, err := request.RequireString("root_dir")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'root_dir' argument"), nil
	}

	question, err := request.RequireString("question")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'question' argument"), nil
	}

	// Get wait parameter (default: true)
	wait := true
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if w, exists := arguments["wait"]; exists {
			if wBool, ok := w.(bool); ok {
				wait = wBool
			}
		}
	}

	// Get timeout parameter
	timeout := time.Duration(0) // Default: no timeout (wait indefinitely)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if t, exists := arguments["timeout"]; exists {
			if tFloat, ok := t.(float64); ok {
				timeoutMs := int64(tFloat)
				if timeoutMs > 0 {
					timeout = time.Duration(timeoutMs) * time.Millisecond
				}
			}
		}
	}

	// Extract session ID for "from" field
	sessionID := ExtractSessionFromContext(ctx)
	from := fmt.Sprintf("Session %s", sessionID)
	if sessionID == "" {
		from = "Anonymous"
	}

	var qa *QuestionAnswer
	var err2 error

	if !wait {
		// Non-blocking mode: submit question and return immediately
		qa, err2 = agentQARegistry.AskQuestionAsync(from, specialty, rootDir, question)
	} else {
		// Blocking mode: wait for answer
		qa, err2 = agentQARegistry.AskQuestion(from, specialty, rootDir, question, timeout)
	}

	if err2 != nil {
		// Still return the Q&A info even on error
		if qa != nil {
			result := map[string]any{
				"question_id": qa.ID,
				"status":      string(qa.Status),
				"error":       err2.Error(),
			}
			resultBytes, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(resultBytes)), nil
		}
		return mcp.NewToolResultError(err2.Error()), nil
	}

	result := map[string]any{
		"question_id": qa.ID,
		"status":      string(qa.Status),
	}

	// Only include answer if we waited for it and it's available
	if wait && qa.Status == QAStatusCompleted {
		result["answer"] = qa.Answer
		result["processing_time"] = qa.ProcessingTime.String()
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

// handleListSpecialists lists all directories with their waiting specialists
func handleListSpecialists(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get all directories
	directories := agentQARegistry.ListDirectories()

	result := make([]map[string]any, 0, len(directories))
	for _, dir := range directories {
		// Count pending questions in directory queue
		pendingCount := 0
		if qaList := agentQARegistry.GetQAsByDirectory(dir.Key); qaList != nil {
			for _, qa := range qaList {
				if qa.Status == QAStatusPending {
					pendingCount++
				}
			}
		}

		result = append(result, map[string]any{
			"key":               dir.Key,
			"root_dir":          dir.RootDir,
			"specialty":         dir.Specialty,
			"instruction":       dir.Instruction,
			"pending_questions": pendingCount,
			"created_at":        dir.CreatedAt.Format(time.RFC3339),
		})
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

// handleGetAnswer retrieves the answer for a previously asked question
func handleGetAnswer(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	questionID, err := request.RequireString("question_id")
	if err != nil {
		return mcp.NewToolResultError("Missing or invalid 'question_id' argument"), nil
	}

	// Get timeout parameter
	timeout := time.Duration(0) // Default: no timeout (wait indefinitely)
	if arguments, ok := request.Params.Arguments.(map[string]any); ok {
		if t, exists := arguments["timeout"]; exists {
			if tFloat, ok := t.(float64); ok {
				timeoutMs := int64(tFloat)
				if timeoutMs > 0 {
					timeout = time.Duration(timeoutMs) * time.Millisecond
				}
			}
		}
	}

	qa, err := agentQARegistry.GetAnswer(questionID, timeout)
	if err != nil {
		// Still return the Q&A info even on error
		if qa != nil {
			result := map[string]any{
				"question_id":     qa.ID,
				"question":        qa.Question,
				"status":          string(qa.Status),
				"timestamp":       qa.Timestamp.Format(time.RFC3339),
				"processing_time": qa.ProcessingTime.String(),
				"error":           err.Error(),
			}
			if qa.Answer != "" {
				result["answer"] = qa.Answer
			}
			resultBytes, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(resultBytes)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"question_id":     qa.ID,
		"question":        qa.Question,
		"status":          string(qa.Status),
		"timestamp":       qa.Timestamp.Format(time.RFC3339),
		"processing_time": qa.ProcessingTime.String(),
	}

	if qa.Answer != "" {
		result["answer"] = qa.Answer
	}

	if qa.Error != "" {
		result["error"] = qa.Error
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}
