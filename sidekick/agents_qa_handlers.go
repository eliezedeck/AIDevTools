package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// handleRegisterSpecialist registers a specialist agent
func handleRegisterSpecialist(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	// Extract session ID from context (for SSE mode)
	sessionID := ExtractSessionFromContext(ctx)

	agent, err := agentQARegistry.RegisterSpecialist(name, specialty, rootDir, sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"id":        agent.ID,
		"name":      agent.Name,
		"specialty": agent.Specialty,
		"root_dir":  agent.RootDir,
		"status":    agent.Status,
	}

	resultBytes, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultBytes)), nil
}

// handleAnswerQuestion provides an answer to a question. A question can only be answered once and only once.
func handleAnswerQuestion(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract session ID to find specialist
	sessionID := ExtractSessionFromContext(ctx)

	// Validate session is still active
	if sessionID == "" {
		return mcp.NewToolResultError("No active session found"), nil
	}

	// Check if session is still connected
	if !sessionManager.IsSessionActive(sessionID) {
		return mcp.NewToolResultError("Session is no longer active"), nil
	}

	// Find which specialty this session is registered for
	var specialty string
	for _, agent := range agentQARegistry.ListSpecialists() {
		if agent.SessionID == sessionID {
			specialty = agent.Specialty
			break
		}
	}

	if specialty == "" {
		return mcp.NewToolResultError("No specialist registered for this session"), nil
	}

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
	// Extract session ID to find specialist
	sessionID := ExtractSessionFromContext(ctx)

	// Validate session is still active
	if sessionID == "" {
		return mcp.NewToolResultError("No active session found"), nil
	}

	// Check if session is still connected
	if !sessionManager.IsSessionActive(sessionID) {
		return mcp.NewToolResultError("Session is no longer active"), nil
	}

	// Find which specialty this session is registered for
	var specialty string
	for _, agent := range agentQARegistry.ListSpecialists() {
		if agent.SessionID == sessionID {
			specialty = agent.Specialty
			break
		}
	}

	if specialty == "" {
		return mcp.NewToolResultError("No specialist registered for this session"), nil
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
		qa, err := agentQARegistry.WaitForQuestionWithContext(ctx, specialty, 1*time.Millisecond)
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

	// Create a context that respects both the request context and session context
	waitCtx := ctx
	if session, exists := sessionManager.GetSession(sessionID); exists && session != nil && session.Context != nil {
		// Use a context that cancels when either the request context or session context is cancelled
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithCancel(ctx)
		defer cancel()

		go func() {
			select {
			case <-session.Context.Done():
				cancel()
			case <-ctx.Done():
				cancel()
			case <-waitCtx.Done():
				return
			}
		}()
	}

	// Wait for next question with context cancellation support
	LogInfo("AgentQA", "Waiting for next question", fmt.Sprintf("Specialty: %s, Timeout: %v", specialty, timeout))

	qa, err := agentQARegistry.WaitForQuestionWithContext(waitCtx, specialty, timeout)
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
		qa, err2 = agentQARegistry.AskQuestionAsync(from, specialty, question)
	} else {
		// Blocking mode: wait for answer
		qa, err2 = agentQARegistry.AskQuestion(from, specialty, question, timeout)
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

// handleListSpecialists lists all available specialists
func handleListSpecialists(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	specialists := agentQARegistry.ListSpecialists()

	result := make([]map[string]any, 0, len(specialists))
	for _, agent := range specialists {
		result = append(result, map[string]any{
			"id":        agent.ID,
			"name":      agent.Name,
			"specialty": agent.Specialty,
			"root_dir":  agent.RootDir,
			"status":    agent.Status,
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
