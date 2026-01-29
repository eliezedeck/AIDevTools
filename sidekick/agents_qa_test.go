package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestContextCancellationHandling tests that the system properly handles context cancellation
func TestContextCancellationHandling(t *testing.T) {
	registry := NewAgentQARegistry()

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start a specialist waiting for questions
	specialistDone := make(chan error, 1)
	go func() {
		_, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Test instructions", 0)
		specialistDone <- err
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

	// Wait for specialist to exit
	select {
	case err := <-specialistDone:
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for specialist to exit after context cancellation")
	}

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify the specialist was cleaned up
	health = registry.GetSystemHealth()
	activeWaiters = health["active_waiters_count"].(int)
	if activeWaiters != 0 {
		t.Errorf("Expected 0 active waiters after cancellation, got %d", activeWaiters)
	}
}

// TestChannelRecreationAfterCancellation tests that new specialists can register after previous one is cancelled
func TestChannelRecreationAfterCancellation(t *testing.T) {
	registry := NewAgentQARegistry()

	// First specialist
	ctx1, cancel1 := context.WithCancel(context.Background())

	specialist1Done := make(chan struct{}, 1)
	go func() {
		registry.WaitForQuestionWithContext(ctx1, "Specialist1", "testing", "/test", "Test instructions", 0)
		specialist1Done <- struct{}{}
	}()

	time.Sleep(100 * time.Millisecond)

	// Cancel first specialist
	cancel1()

	// Wait for first specialist to exit
	select {
	case <-specialist1Done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first specialist to exit")
	}

	time.Sleep(100 * time.Millisecond)

	// Second specialist should be able to register and get questions
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	questionReceived := make(chan bool, 1)

	go func() {
		qa, err := registry.WaitForQuestionWithContext(ctx2, "Specialist2", "testing", "/test", "Test instructions", 2*time.Second)
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
	case <-time.After(3 * time.Second):
		t.Error("Timeout waiting for question to be received")
	}
}

// TestHealthMonitoring tests the health monitoring functionality
func TestHealthMonitoring(t *testing.T) {
	registry := NewAgentQARegistry()

	// Create a directory with a pending question but no active waiter
	dirKey := "/test-testing"
	registry.mutex.Lock()
	registry.directories[dirKey] = &SpecialistDirectory{
		Key:         dirKey,
		RootDir:     "/test",
		Specialty:   "testing",
		Instruction: "Test",
		CreatedAt:   time.Now(),
	}

	// Add a pending question using the new structure
	qa := &QuestionAnswer{
		ID:           "test-qa-1",
		From:         "TestUser",
		Question:     "Test question",
		Status:       QAStatusPending,
		Timestamp:    time.Now(),
		DirectoryKey: dirKey,
	}
	registry.qaIndex["test-qa-1"] = qa
	registry.questionQueues[dirKey] = append(registry.questionQueues[dirKey], qa)
	registry.mutex.Unlock()

	// Run health check - should detect the issue (no active waiter with pending questions)
	// This is mainly to ensure the health check doesn't panic
	registry.checkSystemHealth()
}

// TestSameSpecialistReentry tests that the same specialist can re-enter the wait
func TestSameSpecialistReentry(t *testing.T) {
	registry := NewAgentQARegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First, post a question
	qa1, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Question 1")
	if err != nil {
		t.Fatalf("Failed to ask question: %v", err)
	}

	// Specialist gets the question
	receivedQA, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to wait for question: %v", err)
	}
	if receivedQA.ID != qa1.ID {
		t.Errorf("Expected question %s, got %s", qa1.ID, receivedQA.ID)
	}

	// Specialist answers the question
	err = registry.AnswerQuestion(qa1.ID, "Answer 1", nil)
	if err != nil {
		t.Fatalf("Failed to answer question: %v", err)
	}

	// Post another question
	qa2, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Question 2")
	if err != nil {
		t.Fatalf("Failed to ask second question: %v", err)
	}

	// Same specialist re-enters to get the next question (should not fail)
	receivedQA2, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 1*time.Second)
	if err != nil {
		t.Fatalf("Specialist failed to re-enter: %v", err)
	}
	if receivedQA2.ID != qa2.ID {
		t.Errorf("Expected question %s, got %s", qa2.ID, receivedQA2.ID)
	}
}

// TestOrphanRecovery tests that orphaned questions are recovered when specialist reconnects
func TestOrphanRecovery(t *testing.T) {
	registry := NewAgentQARegistry()

	// First specialist takes a question then "crashes" (context cancelled)
	ctx1, cancel1 := context.WithCancel(context.Background())

	// Post a question
	qa, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Test question")
	if err != nil {
		t.Fatalf("Failed to ask question: %v", err)
	}

	// First specialist gets the question
	specialist1Done := make(chan *QuestionAnswer, 1)
	go func() {
		receivedQA, err := registry.WaitForQuestionWithContext(ctx1, "Specialist1", "testing", "/test", "Instructions", 2*time.Second)
		if err != nil {
			specialist1Done <- nil
		} else {
			specialist1Done <- receivedQA
		}
	}()

	// Wait for specialist to pick up the question
	select {
	case received := <-specialist1Done:
		if received == nil {
			t.Fatal("First specialist failed to get question")
		}
		if received.ID != qa.ID {
			t.Errorf("Expected question %s, got %s", qa.ID, received.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for first specialist")
	}

	// Question should now be in Processing status
	registry.mutex.Lock()
	if qa.Status != QAStatusProcessing {
		t.Errorf("Expected Processing status, got %s", qa.Status)
	}
	registry.mutex.Unlock()

	// Cancel first specialist's context (simulating crash)
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Second specialist should be able to pick up the orphaned question
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	receivedQA2, err := registry.WaitForQuestionWithContext(ctx2, "Specialist2", "testing", "/test", "Instructions", 2*time.Second)
	if err != nil {
		t.Fatalf("Second specialist failed to get recovered question: %v", err)
	}

	// Should be the same question (recovered)
	if receivedQA2.ID != qa.ID {
		t.Errorf("Expected recovered question %s, got %s", qa.ID, receivedQA2.ID)
	}
}

// TestConcurrentQuestionsAndAnswers tests concurrent operations
func TestConcurrentQuestionsAndAnswers(t *testing.T) {
	registry := NewAgentQARegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const numQuestions = 5
	var wg sync.WaitGroup

	// Start specialist
	specialistDone := make(chan struct{})
	go func() {
		defer close(specialistDone)
		for i := 0; i < numQuestions; i++ {
			qa, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 5*time.Second)
			if err != nil {
				t.Errorf("Specialist error: %v", err)
				return
			}
			// Answer the question
			err = registry.AnswerQuestion(qa.ID, "Answer: "+qa.Question, nil)
			if err != nil {
				t.Errorf("Failed to answer: %v", err)
			}
		}
	}()

	// Post questions concurrently
	answers := make(chan string, numQuestions)
	for i := 0; i < numQuestions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			qa, err := registry.AskQuestion("TestUser", "testing", "/test", "Question "+string(rune('A'+idx)), 5*time.Second)
			if err != nil {
				t.Errorf("Failed to ask question: %v", err)
				return
			}
			if qa.Status != QAStatusCompleted {
				t.Errorf("Expected Completed status, got %s", qa.Status)
				return
			}
			answers <- qa.Answer
		}(i)
	}

	wg.Wait()
	close(answers)

	// Verify we got all answers
	count := 0
	for range answers {
		count++
	}
	if count != numQuestions {
		t.Errorf("Expected %d answers, got %d", numQuestions, count)
	}

	// Wait for specialist to finish
	select {
	case <-specialistDone:
		// OK
	case <-time.After(10 * time.Second):
		t.Error("Timeout waiting for specialist to finish")
	}
}

// TestLateAnswerRetrieval tests that late answers can be retrieved via GetAnswer
func TestLateAnswerRetrieval(t *testing.T) {
	registry := NewAgentQARegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ask a question with a short timeout
	qa, err := registry.AskQuestion("TestUser", "testing", "/test", "Test question", 100*time.Millisecond)
	if err == nil {
		// Should timeout since no specialist is waiting
		t.Error("Expected timeout error")
	}
	if qa == nil {
		t.Fatal("Expected qa to be returned even on timeout")
	}

	// Start specialist to answer the question
	go func() {
		receivedQA, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 2*time.Second)
		if err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond) // Simulate processing time
		registry.AnswerQuestion(receivedQA.ID, "Late answer", nil)
	}()

	// Wait for specialist to answer
	time.Sleep(500 * time.Millisecond)

	// Now retrieve the late answer via GetAnswer
	retrievedQA, err := registry.GetAnswer(qa.ID, 1*time.Second)
	if err != nil {
		t.Errorf("Failed to get late answer: %v", err)
	}
	if retrievedQA.Status != QAStatusCompleted {
		t.Errorf("Expected Completed status, got %s", retrievedQA.Status)
	}
	if retrievedQA.Answer != "Late answer" {
		t.Errorf("Expected 'Late answer', got '%s'", retrievedQA.Answer)
	}
}

// TestNoNewAssignmentWhileProcessing tests that a specialist cannot be assigned a new question
// while they still have one in Processing status (one in-flight question per specialist rule)
func TestNoNewAssignmentWhileProcessing(t *testing.T) {
	registry := NewAgentQARegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Post two questions
	qa1, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Question 1")
	if err != nil {
		t.Fatalf("Failed to ask first question: %v", err)
	}
	qa2, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Question 2")
	if err != nil {
		t.Fatalf("Failed to ask second question: %v", err)
	}

	// Specialist gets first question
	receivedQA1, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to get first question: %v", err)
	}
	if receivedQA1.ID != qa1.ID {
		t.Errorf("Expected first question %s, got %s", qa1.ID, receivedQA1.ID)
	}

	// Verify first question is now Processing
	registry.mutex.Lock()
	if qa1.Status != QAStatusProcessing {
		t.Errorf("Expected first question to be Processing, got %s", qa1.Status)
	}
	registry.mutex.Unlock()

	// Same specialist tries to get next question WITHOUT answering first
	// Should timeout because they already have one in Processing status
	_, err = registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 200*time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout - specialist should not get new question while one is Processing")
	}

	// Verify second question is still Pending (not assigned)
	registry.mutex.Lock()
	if qa2.Status != QAStatusPending {
		t.Errorf("Expected second question to still be Pending, got %s", qa2.Status)
	}
	registry.mutex.Unlock()

	// Now specialist answers first question
	err = registry.AnswerQuestion(qa1.ID, "Answer 1", nil)
	if err != nil {
		t.Fatalf("Failed to answer first question: %v", err)
	}

	// Now specialist should be able to get second question
	receivedQA2, err := registry.WaitForQuestionWithContext(ctx, "TestSpecialist", "testing", "/test", "Instructions", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to get second question after answering first: %v", err)
	}
	if receivedQA2.ID != qa2.ID {
		t.Errorf("Expected second question %s, got %s", qa2.ID, receivedQA2.ID)
	}
}

// TestSameSpecialistReentryWithCancelledContext tests that the same specialist can re-register
// when their previous context was cancelled
func TestSameSpecialistReentryWithCancelledContext(t *testing.T) {
	registry := NewAgentQARegistry()

	// First context for the specialist
	ctx1, cancel1 := context.WithCancel(context.Background())

	// Post a question
	qa, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Test question")
	if err != nil {
		t.Fatalf("Failed to ask question: %v", err)
	}

	// Specialist registers with first context
	specialist1Done := make(chan *QuestionAnswer, 1)
	go func() {
		receivedQA, err := registry.WaitForQuestionWithContext(ctx1, "TestSpecialist", "testing", "/test", "Instructions", 2*time.Second)
		if err != nil {
			specialist1Done <- nil
		} else {
			specialist1Done <- receivedQA
		}
	}()

	// Wait for specialist to pick up the question
	select {
	case received := <-specialist1Done:
		if received == nil {
			t.Fatal("Specialist failed to get question")
		}
		if received.ID != qa.ID {
			t.Errorf("Expected question %s, got %s", qa.ID, received.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for specialist")
	}

	// Verify specialist is registered
	registry.mutex.Lock()
	waiter, exists := registry.activeWaiters["/test-testing"]
	if !exists {
		t.Fatal("Expected active waiter to exist")
	}
	if waiter.Name != "TestSpecialist" {
		t.Errorf("Expected waiter name 'TestSpecialist', got '%s'", waiter.Name)
	}
	registry.mutex.Unlock()

	// Cancel the first context (simulating disconnect/reconnect)
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Answer the question (specialist completed work before context cancelled)
	err = registry.AnswerQuestion(qa.ID, "Answer", nil)
	if err != nil {
		t.Fatalf("Failed to answer question: %v", err)
	}

	// Post another question
	qa2, err := registry.AskQuestionAsync("TestUser", "testing", "/test", "Question 2")
	if err != nil {
		t.Fatalf("Failed to ask second question: %v", err)
	}

	// Same specialist tries to re-enter with a NEW context
	// This should succeed because the old context was cancelled
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	receivedQA2, err := registry.WaitForQuestionWithContext(ctx2, "TestSpecialist", "testing", "/test", "Instructions", 2*time.Second)
	if err != nil {
		t.Fatalf("Same specialist with new context failed to re-register: %v", err)
	}
	if receivedQA2.ID != qa2.ID {
		t.Errorf("Expected question %s, got %s", qa2.ID, receivedQA2.ID)
	}

	// Verify the waiter now has the new context
	registry.mutex.Lock()
	waiter, exists = registry.activeWaiters["/test-testing"]
	if !exists {
		t.Fatal("Expected active waiter to exist after re-registration")
	}
	// Check context is NOT cancelled (new context)
	select {
	case <-waiter.Context.Done():
		t.Error("Expected new waiter context to be active, but it's cancelled")
	default:
		// Good - context is active
	}
	registry.mutex.Unlock()
}
