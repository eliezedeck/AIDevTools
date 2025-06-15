package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// QAStatus represents the status of a Q&A exchange
type QAStatus string

const (
	QAStatusPending    QAStatus = "Pending"
	QAStatusProcessing QAStatus = "Processing"
	QAStatusCompleted  QAStatus = "Completed"
	QAStatusFailed     QAStatus = "Failed"
	QAStatusTimeout    QAStatus = "Timeout"
)

// QuestionAnswer represents a Q&A exchange between agents
type QuestionAnswer struct {
	ID             string
	From           string // Requesting agent
	To             string // Specialist agent
	Question       string
	Answer         string
	Error          string
	Status         QAStatus
	Timestamp      time.Time
	ProcessingTime time.Duration
	ExpiresAt      time.Time // When this Q&A entry expires (6 hours after creation)
}

// SpecialistAgent represents a registered specialist agent
type SpecialistAgent struct {
	ID        string
	Name      string
	Specialty string
	RootDir   string // Root directory of the project this specialist is specialized in
	SessionID string // MCP session ID
	ProcessID string // If spawned via sidekick
	Status    string // "available", "busy", "offline"
}

// AgentQARegistry manages Q&A exchanges and specialist registrations
type AgentQARegistry struct {
	specialists map[string]*SpecialistAgent     // key: specialty
	qaHistory   map[string]*QuestionAnswer      // key: Q&A ID
	qaQueues    map[string]chan *QuestionAnswer // key: specialty
	waiters     map[string]chan *QuestionAnswer // key: Q&A ID, for answer responses
	mutex       sync.RWMutex
}

// NewAgentQARegistry creates a new agent Q&A registry
func NewAgentQARegistry() *AgentQARegistry {
	r := &AgentQARegistry{
		specialists: make(map[string]*SpecialistAgent),
		qaHistory:   make(map[string]*QuestionAnswer),
		qaQueues:    make(map[string]chan *QuestionAnswer),
		waiters:     make(map[string]chan *QuestionAnswer),
	}
	// Start cleanup routine for expired Q&A entries
	r.startCleanupRoutine()
	return r
}

// RegisterSpecialist registers a specialist agent
func (r *AgentQARegistry) RegisterSpecialist(name, specialty, rootDir, sessionID string) (*SpecialistAgent, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Check if specialty already registered
	if existing, exists := r.specialists[specialty]; exists {
		return nil, fmt.Errorf("specialty '%s' already registered by agent '%s'", specialty, existing.Name)
	}

	agent := &SpecialistAgent{
		ID:        uuid.New().String(),
		Name:      name,
		Specialty: specialty,
		RootDir:   rootDir,
		SessionID: sessionID,
		Status:    "available",
	}

	r.specialists[specialty] = agent

	// Create question queue for this specialty
	r.qaQueues[specialty] = make(chan *QuestionAnswer, 100)

	LogInfo("AgentQA", fmt.Sprintf("Registered specialist '%s' for '%s'", name, specialty))

	return agent, nil
}

// UnregisterSpecialist removes a specialist agent
func (r *AgentQARegistry) UnregisterSpecialist(specialty string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if agent, exists := r.specialists[specialty]; exists {
		delete(r.specialists, specialty)

		// Close the question queue
		if queue, exists := r.qaQueues[specialty]; exists {
			close(queue)
			delete(r.qaQueues, specialty)
		}

		LogInfo("AgentQA", fmt.Sprintf("Unregistered specialist '%s' for '%s'", agent.Name, specialty))
	}
}

// GetSpecialist returns a specialist for a given specialty
func (r *AgentQARegistry) GetSpecialist(specialty string) *SpecialistAgent {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.specialists[specialty]
}

// ListSpecialists returns all registered specialists
func (r *AgentQARegistry) ListSpecialists() []*SpecialistAgent {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	agents := make([]*SpecialistAgent, 0, len(r.specialists))
	for _, agent := range r.specialists {
		agents = append(agents, agent)
	}

	// Sort by specialty
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Specialty < agents[j].Specialty
	})

	return agents
}

// AskQuestion submits a question to a specialist
func (r *AgentQARegistry) AskQuestion(from, specialty, question string, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.Lock()

	// Check if specialist exists
	specialist := r.specialists[specialty]
	if specialist == nil {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no specialist registered for '%s'", specialty)
	}

	// Get the queue for this specialty
	queue, exists := r.qaQueues[specialty]
	if !exists {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no queue for specialty '%s'", specialty)
	}

	// Create Q&A entry
	qa := &QuestionAnswer{
		ID:        uuid.New().String(),
		From:      from,
		To:        specialist.Name,
		Question:  question,
		Status:    QAStatusPending,
		Timestamp: time.Now(),
		ExpiresAt: time.Now().Add(6 * time.Hour), // Expires after 6 hours
	}

	// Store in history
	r.qaHistory[qa.ID] = qa

	// Create response channel
	responseChan := make(chan *QuestionAnswer, 1)
	r.waiters[qa.ID] = responseChan

	r.mutex.Unlock()

	// Send question to specialist's queue
	select {
	case queue <- qa:
		// Question sent successfully
		LogInfo("AgentQA", fmt.Sprintf("Question %s sent to specialist '%s'", qa.ID, specialist.Name))
	default:
		// Queue is full
		r.mutex.Lock()
		qa.Status = QAStatusFailed
		qa.Error = "Specialist queue is full"
		delete(r.waiters, qa.ID)
		r.mutex.Unlock()
		return qa, fmt.Errorf("specialist queue is full")
	}

	// Wait for response with timeout
	if timeout == 0 {
		// No timeout - wait indefinitely
		updatedQA := <-responseChan
		return updatedQA, nil
	} else {
		// With timeout
		select {
		case updatedQA := <-responseChan:
			return updatedQA, nil
		case <-time.After(timeout):
			r.mutex.Lock()
			qa.Status = QAStatusTimeout
			qa.Error = "Timeout waiting for response"
			delete(r.waiters, qa.ID)
			r.mutex.Unlock()
			return qa, fmt.Errorf("timeout waiting for response")
		}
	}
}

// WaitForQuestion waits for a question for a specialist (blocking)
func (r *AgentQARegistry) WaitForQuestion(specialty string, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.RLock()

	// Check if specialist exists
	specialist := r.specialists[specialty]
	if specialist == nil {
		r.mutex.RUnlock()
		return nil, fmt.Errorf("specialist not registered for '%s'", specialty)
	}

	// Get the queue for this specialty
	queue, exists := r.qaQueues[specialty]
	if !exists {
		r.mutex.RUnlock()
		return nil, fmt.Errorf("no queue for specialty '%s'", specialty)
	}

	// Update specialist status
	specialist.Status = "available"
	r.mutex.RUnlock()

	// Wait for question
	if timeout == 0 {
		// No timeout - block indefinitely
		qa := <-queue
		r.mutex.Lock()
		qa.Status = QAStatusProcessing
		specialist.Status = "busy"
		r.mutex.Unlock()
		return qa, nil
	} else {
		// With timeout
		select {
		case qa := <-queue:
			r.mutex.Lock()
			qa.Status = QAStatusProcessing
			specialist.Status = "busy"
			r.mutex.Unlock()
			return qa, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("timeout waiting for question")
		}
	}
}

// AnswerQuestion provides an answer to a question
func (r *AgentQARegistry) AnswerQuestion(questionID, answer string, err error) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Get the Q&A entry
	qa, exists := r.qaHistory[questionID]
	if !exists {
		return fmt.Errorf("question ID '%s' not found", questionID)
	}

	// Update Q&A entry
	qa.ProcessingTime = time.Since(qa.Timestamp)

	if err != nil {
		qa.Status = QAStatusFailed
		qa.Error = err.Error()
	} else {
		qa.Status = QAStatusCompleted
		qa.Answer = answer
	}

	// Update specialist status
	if specialist, exists := r.specialists[qa.To]; exists {
		specialist.Status = "available"
	}

	// Send response to waiting channel
	if waiter, exists := r.waiters[questionID]; exists {
		select {
		case waiter <- qa:
			// Response sent
		default:
			// Waiter not listening anymore
		}
		close(waiter)
		delete(r.waiters, questionID)
	}

	LogInfo("AgentQA", fmt.Sprintf("Question %s answered by '%s'", questionID, qa.To))

	return nil
}

// GetQA returns a specific Q&A entry
func (r *AgentQARegistry) GetQA(id string) *QuestionAnswer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.qaHistory[id]
}

// GetAllQAs returns all Q&A entries sorted by timestamp (newest first)
func (r *AgentQARegistry) GetAllQAs() []*QuestionAnswer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	qas := make([]*QuestionAnswer, 0, len(r.qaHistory))
	for _, qa := range r.qaHistory {
		qas = append(qas, qa)
	}

	// Sort by timestamp (newest first)
	sort.Slice(qas, func(i, j int) bool {
		return qas[i].Timestamp.After(qas[j].Timestamp)
	})

	return qas
}

// CleanupForSession removes all specialists and Q&As for a given session
func (r *AgentQARegistry) CleanupForSession(sessionID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Find and remove specialists for this session
	for specialty, agent := range r.specialists {
		if agent.SessionID == sessionID {
			delete(r.specialists, specialty)

			// Close the question queue
			if queue, exists := r.qaQueues[specialty]; exists {
				close(queue)
				delete(r.qaQueues, specialty)
			}

			LogInfo("AgentQA", fmt.Sprintf("Cleaned up specialist '%s' for session %s", agent.Name, sessionID))
		}
	}
}

// AskQuestionAsync submits a question to a specialist and returns immediately with question ID
func (r *AgentQARegistry) AskQuestionAsync(from, specialty, question string) (*QuestionAnswer, error) {
	r.mutex.Lock()

	// Check if specialist exists
	specialist := r.specialists[specialty]
	if specialist == nil {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no specialist registered for '%s'", specialty)
	}

	// Get the queue for this specialty
	queue, exists := r.qaQueues[specialty]
	if !exists {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no queue for specialty '%s'", specialty)
	}

	// Create Q&A entry
	qa := &QuestionAnswer{
		ID:        uuid.New().String(),
		From:      from,
		To:        specialist.Name,
		Question:  question,
		Status:    QAStatusPending,
		Timestamp: time.Now(),
		ExpiresAt: time.Now().Add(6 * time.Hour), // Expires after 6 hours
	}

	// Store in history
	r.qaHistory[qa.ID] = qa

	// Create response channel for future use
	responseChan := make(chan *QuestionAnswer, 1)
	r.waiters[qa.ID] = responseChan

	r.mutex.Unlock()

	// Send question to specialist's queue
	select {
	case queue <- qa:
		// Question sent successfully
		LogInfo("AgentQA", fmt.Sprintf("Question %s sent to specialist '%s'", qa.ID, specialist.Name))
	default:
		// Queue is full
		r.mutex.Lock()
		qa.Status = QAStatusFailed
		qa.Error = "Specialist queue is full"
		delete(r.waiters, qa.ID)
		r.mutex.Unlock()
		return qa, fmt.Errorf("specialist queue is full")
	}

	// Return immediately with the question ID
	return qa, nil
}

// GetAnswer retrieves the answer for a previously asked question
func (r *AgentQARegistry) GetAnswer(questionID string, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.RLock()
	
	// Get the Q&A entry
	qa, exists := r.qaHistory[questionID]
	if !exists {
		r.mutex.RUnlock()
		return nil, fmt.Errorf("question ID '%s' not found", questionID)
	}

	// Check if answer is already available
	if qa.Status == QAStatusCompleted || qa.Status == QAStatusFailed {
		r.mutex.RUnlock()
		return qa, nil
	}

	// Get the waiter channel
	waiter, exists := r.waiters[questionID]
	if !exists {
		// No waiter channel means the question was already answered or timed out
		r.mutex.RUnlock()
		return qa, nil
	}
	r.mutex.RUnlock()

	// Wait for answer with timeout
	if timeout == 0 {
		// No timeout - wait indefinitely
		updatedQA := <-waiter
		return updatedQA, nil
	} else {
		// With timeout
		select {
		case updatedQA := <-waiter:
			return updatedQA, nil
		case <-time.After(timeout):
			// Return the current state of the Q&A
			r.mutex.RLock()
			currentQA := r.qaHistory[questionID]
			r.mutex.RUnlock()
			return currentQA, fmt.Errorf("timeout waiting for answer")
		}
	}
}

// startCleanupRoutine starts a goroutine that periodically cleans up expired Q&A entries
func (r *AgentQARegistry) startCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Run cleanup every hour
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.cleanupExpiredEntries()
			}
		}
	}()
}

// cleanupExpiredEntries removes Q&A entries that have expired
func (r *AgentQARegistry) cleanupExpiredEntries() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()
	expiredCount := 0

	for id, qa := range r.qaHistory {
		if now.After(qa.ExpiresAt) {
			// Clean up waiter channel if exists
			if waiter, exists := r.waiters[id]; exists {
				close(waiter)
				delete(r.waiters, id)
			}
			
			// Remove from history
			delete(r.qaHistory, id)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		LogInfo("AgentQA", fmt.Sprintf("Cleaned up %d expired Q&A entries", expiredCount))
	}
}

// GetQAsBySpecialty returns all Q&A entries for a specific specialty, sorted by timestamp (newest first)
func (r *AgentQARegistry) GetQAsBySpecialty(specialty string) []*QuestionAnswer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Get the specialist for this specialty
	specialist := r.specialists[specialty]
	if specialist == nil {
		return []*QuestionAnswer{}
	}

	qas := make([]*QuestionAnswer, 0)
	for _, qa := range r.qaHistory {
		if qa.To == specialist.Name {
			qas = append(qas, qa)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(qas, func(i, j int) bool {
		return qas[i].Timestamp.After(qas[j].Timestamp)
	})

	return qas
}

// Global registry instance
var agentQARegistry = NewAgentQARegistry()
