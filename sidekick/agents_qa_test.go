package main

import (
	"context"
	"testing"
	"time"
)

// TestContextCancellationHandling tests that the system properly handles context cancellation
func TestContextCancellationHandling(t *testing.T) {
	registry := NewAgentQARegistry()

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start a specialist waiting for questions
	go func() {
		_, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Test instructions", 0)
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}
	}()

	// Give the specialist time to register
	time.Sleep(100 * time.Millisecond)

	// Verify the specialist is registered
	health := registry.GetSystemHealth()
	activeWaiters := health["active_waiters_count"].(int)
	if activeWaiters != 1 {
		t.Errorf("Expected 1 active waiter, got %d", activeWaiters)
	}

	// Cancel the context (simulating sleep/wake cycle)
	cancel()

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Try to ask a question - should fail because no active waiter
	_, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Test question")
	if err == nil {
		t.Error("Expected error when asking question with no active waiter")
	}

	// Verify the error message indicates no active specialist
	expectedError := "no active specialist waiting for questions"
	if err.Error()[:len(expectedError)] != expectedError {
		t.Errorf("Expected error about no active specialist, got: %s", err.Error())
	}
}

// TestChannelRecreationAfterCancellation tests that channels are recreated properly
func TestChannelRecreationAfterCancellation(t *testing.T) {
	registry := NewAgentQARegistry()

	// First specialist
	ctx1, cancel1 := context.WithCancel(context.Background())

	go func() {
		registry.WaitForQuestionWithContext(ctx1, "Specialist1", "testing", "/test", "Test instructions", 0)
	}()

	time.Sleep(100 * time.Millisecond)

	// Cancel first specialist
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Second specialist should be able to register and get questions
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	questionReceived := make(chan bool, 1)

	go func() {
		qa, err := registry.WaitForQuestionWithContext(ctx2, "Specialist2", "testing", "/test", "Test instructions", 1*time.Second)
		if err == nil && qa != nil {
			questionReceived <- true
		} else {
			questionReceived <- false
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Send a question
	_, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Test question")
	if err != nil {
		t.Errorf("Failed to ask question: %v", err)
	}

	// Verify question was received
	select {
	case received := <-questionReceived:
		if !received {
			t.Error("Question was not received by second specialist")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for question to be received")
	}
}

// TestHealthMonitoring tests the health monitoring functionality
func TestHealthMonitoring(t *testing.T) {
	registry := NewAgentQARegistry()

	// Create a directory with a pending question but no active waiter
	dirKey := "/test-testing"
	registry.directories[dirKey] = &SpecialistDirectory{
		Key:         dirKey,
		RootDir:     "/test",
		Specialty:   "testing",
		Instruction: "Test",
		CreatedAt:   time.Now(),
	}

	// Add a pending question
	qa := &QuestionAnswer{
		ID:           "test-qa-1",
		From:         "TestUser",
		Question:     "Test question",
		Status:       QAStatusPending,
		Timestamp:    time.Now(),
		DirectoryKey: dirKey,
	}
	registry.qaHistory["test-qa-1"] = qa

	// Run health check
	registry.checkSystemHealth()

	// Health check should detect the issue (no active waiter with pending questions)
	// This is mainly to ensure the health check doesn't panic
}
