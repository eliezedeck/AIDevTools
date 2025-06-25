package main

import (
	"context"
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
	DirectoryKey   string    // The directory this question belongs to
}

// SpecialistAgent represents a registered specialist agent
type SpecialistAgent struct {
	ID          string
	Name        string
	Specialty   string
	RootDir     string    // Root directory of the project this specialist is specialized in
	Instruction string    // Usage guidance for potential questioners
	LastSeen    time.Time // Track when specialist last called get_next_question
}

// SpecialistDirectory represents a directory where specialists can answer questions
type SpecialistDirectory struct {
	Key         string    // "<root_dir>-<specialty>"
	RootDir     string    // Project root directory
	Specialty   string    // Area of expertise
	Instruction string    // Usage guidance
	CreatedAt   time.Time // When directory was created
}

// ActiveWaiter tracks an active specialist waiting for questions
type ActiveWaiter struct {
	Name       string
	Context    context.Context
	Cancel     context.CancelFunc
	LastSeen   time.Time
	ChannelGen int64 // Generation number for channel recreation detection
}

// AgentQARegistry manages Q&A exchanges and specialist registrations
type AgentQARegistry struct {
	directories   map[string]*SpecialistDirectory // key: "<root-dir>-<specialty>"
	qaHistory     map[string]*QuestionAnswer      // key: Q&A ID
	qaQueues      map[string]chan *QuestionAnswer // key: "<root-dir>-<specialty>" (directory queues)
	waiters       map[string]chan *QuestionAnswer // key: Q&A ID, for answer responses
	activeWaiters map[string]*ActiveWaiter        // key: "<root-dir>-<specialty>", tracks active specialists
	mutex         sync.RWMutex
}

// NewAgentQARegistry creates a new agent Q&A registry
func NewAgentQARegistry() *AgentQARegistry {
	r := &AgentQARegistry{
		directories:   make(map[string]*SpecialistDirectory),
		qaHistory:     make(map[string]*QuestionAnswer),
		qaQueues:      make(map[string]chan *QuestionAnswer),
		waiters:       make(map[string]chan *QuestionAnswer),
		activeWaiters: make(map[string]*ActiveWaiter),
	}
	// Start cleanup routine for expired Q&A entries
	r.startCleanupRoutine()
	// Start waiter cleanup routine for orphaned specialists
	r.startWaiterCleanupRoutine()
	// Start health monitoring routine
	r.startHealthMonitoringRoutine()
	return r
}

// GetDirectoryBySpecialty returns the first directory for a given specialty
func (r *AgentQARegistry) GetDirectoryBySpecialty(specialty string) *SpecialistDirectory {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Look through all directories to find a matching specialty
	for _, dir := range r.directories {
		if dir.Specialty == specialty {
			return dir
		}
	}
	return nil
}

// GetDirectory returns a directory by key
func (r *AgentQARegistry) GetDirectory(key string) *SpecialistDirectory {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.directories[key]
}

// ListDirectories returns all directories
func (r *AgentQARegistry) ListDirectories() []*SpecialistDirectory {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	dirs := make([]*SpecialistDirectory, 0)
	for _, dir := range r.directories {
		dirs = append(dirs, dir)
	}

	// Sort by specialty, then by root dir
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].Specialty == dirs[j].Specialty {
			return dirs[i].RootDir < dirs[j].RootDir
		}
		return dirs[i].Specialty < dirs[j].Specialty
	})

	return dirs
}

// AskQuestion submits a question to a specialist directory
func (r *AgentQARegistry) AskQuestion(from, specialty, rootDir, question string, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.Lock()

	// Create directory key to find the specific directory
	dirKey := fmt.Sprintf("%s-%s", rootDir, specialty)
	selectedDir := r.directories[dirKey]

	if selectedDir == nil {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no directory available for specialty '%s' in root directory '%s'", specialty, rootDir)
	}

	// Check if there's an active waiter for this directory
	activeWaiter, hasActiveWaiter := r.activeWaiters[dirKey]
	if !hasActiveWaiter {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no active specialist waiting for questions in directory '%s'", dirKey)
	}

	// Check if the active waiter's context is still valid
	select {
	case <-activeWaiter.Context.Done():
		// Active waiter's context is cancelled, clean it up
		LogInfo("AgentQA", fmt.Sprintf("Active waiter context cancelled for directory '%s', cleaning up", dirKey))
		if activeWaiter.Cancel != nil {
			activeWaiter.Cancel()
		}
		delete(r.activeWaiters, dirKey)
		r.mutex.Unlock()
		return nil, fmt.Errorf("active specialist context cancelled for directory '%s'", dirKey)
	default:
		// Active waiter is still valid
	}

	// Get the directory queue
	queue, exists := r.qaQueues[selectedDir.Key]
	if !exists {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no queue for directory '%s'", selectedDir.Key)
	}

	// Create Q&A entry
	qa := &QuestionAnswer{
		ID:           uuid.New().String(),
		From:         from,
		To:           specialty, // Will be updated by the specialist who picks it up
		Question:     question,
		Status:       QAStatusPending,
		Timestamp:    time.Now(),
		ExpiresAt:    time.Now().Add(6 * time.Hour), // Expires after 6 hours
		DirectoryKey: selectedDir.Key,
	}

	// Store in history
	r.qaHistory[qa.ID] = qa

	// Create response channel
	responseChan := make(chan *QuestionAnswer, 1)
	r.waiters[qa.ID] = responseChan

	r.mutex.Unlock()

	// Send question to directory queue with panic recovery
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				EmergencyLog("AgentQA", "Panic sending question to directory", fmt.Sprintf("Question: %s, Directory: %s, Panic: %v", qa.ID, selectedDir.Key, rec))
			}
		}()

		select {
		case queue <- qa:
			// Question sent successfully
			LogInfo("AgentQA", fmt.Sprintf("Question %s sent to directory '%s' (active waiter: %s)", qa.ID, selectedDir.Key, activeWaiter.Name))
		default:
			// Queue is full
			r.mutex.Lock()
			qa.Status = QAStatusFailed
			qa.Error = "Directory queue is full"
			delete(r.waiters, qa.ID)
			r.mutex.Unlock()
			LogError("AgentQA", "Directory queue is full", fmt.Sprintf("Question: %s, Directory: %s", qa.ID, selectedDir.Key))
		}
	}()

	// Check if question sending failed
	if qa.Status == QAStatusFailed {
		return qa, fmt.Errorf(qa.Error)
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
func (r *AgentQARegistry) WaitForQuestion(name, specialty, rootDir, instructions string, timeout time.Duration) (*QuestionAnswer, error) {
	return r.WaitForQuestionWithContext(context.Background(), name, specialty, rootDir, instructions, timeout)
}

// WaitForQuestionWithContext waits for a question for a specialist with context cancellation support
func (r *AgentQARegistry) WaitForQuestionWithContext(ctx context.Context, name, specialty, rootDir, instructions string, timeout time.Duration) (*QuestionAnswer, error) {
	// Add panic recovery for channel operations
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in WaitForQuestionWithContext", fmt.Sprintf("Specialty: %s, Panic: %v", specialty, rec))
		}
	}()

	// Create directory key
	dirKey := fmt.Sprintf("%s-%s", rootDir, specialty)

	r.mutex.Lock()

	// Check if there's an existing active waiter that needs cleanup
	if existingWaiter, exists := r.activeWaiters[dirKey]; exists {
		// Check if the existing waiter's context is cancelled or expired
		select {
		case <-existingWaiter.Context.Done():
			// Previous waiter's context is cancelled, clean it up
			LogInfo("AgentQA", fmt.Sprintf("Cleaning up cancelled waiter for directory '%s'", dirKey))
			if existingWaiter.Cancel != nil {
				existingWaiter.Cancel()
			}
			delete(r.activeWaiters, dirKey)

			// Recreate the channel to ensure fresh state
			r.qaQueues[dirKey] = make(chan *QuestionAnswer, 100)
			LogInfo("AgentQA", fmt.Sprintf("Recreated channel for directory '%s' after context cancellation", dirKey))
		default:
			// Previous waiter is still active
			r.mutex.Unlock()
			return nil, fmt.Errorf("another specialist is already waiting for questions in directory '%s'", dirKey)
		}
	}

	// Create or update directory
	dir := r.directories[dirKey]
	if dir == nil {
		// Create new directory
		dir = &SpecialistDirectory{
			Key:         dirKey,
			RootDir:     rootDir,
			Specialty:   specialty,
			Instruction: instructions,
			CreatedAt:   time.Now(),
		}
		r.directories[dirKey] = dir

		// Create question queue for this directory
		r.qaQueues[dirKey] = make(chan *QuestionAnswer, 100)

		LogInfo("AgentQA", fmt.Sprintf("Created new directory '%s' with instructions", dirKey))
	} else {
		// Update instructions if provided
		if instructions != "" {
			dir.Instruction = instructions
			LogInfo("AgentQA", fmt.Sprintf("Updated instructions for directory '%s'", dirKey))
		}
	}

	// Ensure queue exists and is fresh
	queue, exists := r.qaQueues[dirKey]
	if !exists {
		// Create new queue
		queue = make(chan *QuestionAnswer, 100)
		r.qaQueues[dirKey] = queue
		LogInfo("AgentQA", fmt.Sprintf("Created new queue for directory '%s'", dirKey))
	}

	// Create a child context for this specific waiter
	waiterCtx, waiterCancel := context.WithCancel(ctx)

	// Register this specialist as an active waiter
	r.activeWaiters[dirKey] = &ActiveWaiter{
		Name:       name,
		Context:    waiterCtx,
		Cancel:     waiterCancel,
		LastSeen:   time.Now(),
		ChannelGen: time.Now().UnixNano(), // Use timestamp as generation
	}

	r.mutex.Unlock()

	// Cleanup function to remove active waiter when done
	defer func() {
		r.mutex.Lock()
		if waiter, exists := r.activeWaiters[dirKey]; exists && waiter.Name == name {
			delete(r.activeWaiters, dirKey)
			LogInfo("AgentQA", fmt.Sprintf("Removed active waiter '%s' from directory '%s'", name, dirKey))
		}
		r.mutex.Unlock()
		waiterCancel()
	}()

	LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' waiting for questions in directory '%s'", name, dirKey))

	// Wait for question with context cancellation support
	if timeout == 0 {
		// No timeout - block until question arrives or context is cancelled
		select {
		case qa, ok := <-queue:
			if !ok {
				return nil, fmt.Errorf("directory queue closed")
			}
			r.mutex.Lock()
			qa.Status = QAStatusProcessing
			// Update the 'To' field with the actual specialist name
			qa.To = name
			// Update last seen time
			if waiter, exists := r.activeWaiters[dirKey]; exists && waiter.Name == name {
				waiter.LastSeen = time.Now()
			}
			r.mutex.Unlock()
			LogInfo("AgentQA", fmt.Sprintf("Question %s assigned to specialist '%s'", qa.ID, name))
			return qa, nil
		case <-waiterCtx.Done():
			LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' context cancelled in directory '%s'", name, dirKey))
			return nil, fmt.Errorf("context cancelled: %w", waiterCtx.Err())
		}
	} else {
		// With timeout and context cancellation
		select {
		case qa, ok := <-queue:
			if !ok {
				return nil, fmt.Errorf("directory queue closed")
			}
			r.mutex.Lock()
			qa.Status = QAStatusProcessing
			// Update the 'To' field with the actual specialist name
			qa.To = name
			// Update last seen time
			if waiter, exists := r.activeWaiters[dirKey]; exists && waiter.Name == name {
				waiter.LastSeen = time.Now()
			}
			r.mutex.Unlock()
			LogInfo("AgentQA", fmt.Sprintf("Question %s assigned to specialist '%s'", qa.ID, name))
			return qa, nil
		case <-time.After(timeout):
			LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' timed out waiting in directory '%s'", name, dirKey))
			return nil, fmt.Errorf("timeout waiting for question")
		case <-waiterCtx.Done():
			LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' context cancelled in directory '%s'", name, dirKey))
			return nil, fmt.Errorf("context cancelled: %w", waiterCtx.Err())
		}
	}
}

// AnswerQuestion provides an answer to a question. A question can only be answered once and only once.
func (r *AgentQARegistry) AnswerQuestion(questionID, answer string, err error) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Get the Q&A entry
	qa, exists := r.qaHistory[questionID]
	if !exists {
		return fmt.Errorf("question ID '%s' not found", questionID)
	}

	// Check if question has already been answered
	if qa.Status == QAStatusCompleted {
		return fmt.Errorf("question ID '%s' has already been answered", questionID)
	}

	if qa.Status == QAStatusFailed {
		return fmt.Errorf("question ID '%s' has already failed and cannot be answered", questionID)
	}

	// Only allow answering questions that are in processing or pending status
	if qa.Status != QAStatusProcessing && qa.Status != QAStatusPending {
		return fmt.Errorf("question ID '%s' is in status '%s' and cannot be answered", questionID, qa.Status)
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

	// No need to update specialist status in the new system

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

// AskQuestionAsync submits a question to a specialist and returns immediately with question ID
func (r *AgentQARegistry) AskQuestionAsync(from, specialty, rootDir, question string) (*QuestionAnswer, error) {
	r.mutex.Lock()

	// Create directory key to find the specific directory
	dirKey := fmt.Sprintf("%s-%s", rootDir, specialty)
	selectedDir := r.directories[dirKey]

	if selectedDir == nil {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no directory available for specialty '%s' in root directory '%s'", specialty, rootDir)
	}

	// Check if there's an active waiter for this directory
	activeWaiter, hasActiveWaiter := r.activeWaiters[dirKey]
	if !hasActiveWaiter {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no active specialist waiting for questions in directory '%s'", dirKey)
	}

	// Check if the active waiter's context is still valid
	select {
	case <-activeWaiter.Context.Done():
		// Active waiter's context is cancelled, clean it up
		LogInfo("AgentQA", fmt.Sprintf("Active waiter context cancelled for directory '%s', cleaning up", dirKey))
		if activeWaiter.Cancel != nil {
			activeWaiter.Cancel()
		}
		delete(r.activeWaiters, dirKey)
		r.mutex.Unlock()
		return nil, fmt.Errorf("active specialist context cancelled for directory '%s'", dirKey)
	default:
		// Active waiter is still valid
	}

	// Get the directory queue
	queue, exists := r.qaQueues[selectedDir.Key]
	if !exists {
		r.mutex.Unlock()
		return nil, fmt.Errorf("no queue for directory '%s'", selectedDir.Key)
	}

	// Create Q&A entry
	qa := &QuestionAnswer{
		ID:           uuid.New().String(),
		From:         from,
		To:           specialty, // Will be updated by the specialist who picks it up
		Question:     question,
		Status:       QAStatusPending,
		Timestamp:    time.Now(),
		ExpiresAt:    time.Now().Add(6 * time.Hour), // Expires after 6 hours
		DirectoryKey: selectedDir.Key,
	}

	// Store in history
	r.qaHistory[qa.ID] = qa

	// Create response channel for future use
	responseChan := make(chan *QuestionAnswer, 1)
	r.waiters[qa.ID] = responseChan

	r.mutex.Unlock()

	// Send question to directory queue with panic recovery
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				EmergencyLog("AgentQA", "Panic sending async question to directory", fmt.Sprintf("Question: %s, Directory: %s, Panic: %v", qa.ID, selectedDir.Key, rec))
			}
		}()

		select {
		case queue <- qa:
			// Question sent successfully
			LogInfo("AgentQA", fmt.Sprintf("Async question %s sent to directory '%s' (active waiter: %s)", qa.ID, selectedDir.Key, activeWaiter.Name))
		default:
			// Queue is full
			r.mutex.Lock()
			qa.Status = QAStatusFailed
			qa.Error = "Directory queue is full"
			delete(r.waiters, qa.ID)
			r.mutex.Unlock()
			LogError("AgentQA", "Directory queue is full", fmt.Sprintf("Async question: %s, Directory: %s", qa.ID, selectedDir.Key))
		}
	}()

	// Check if question sending failed
	if qa.Status == QAStatusFailed {
		return qa, fmt.Errorf(qa.Error)
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

	// Wait for answer with timeout and closed channel handling
	if timeout == 0 {
		// No timeout - wait indefinitely
		select {
		case updatedQA, ok := <-waiter:
			if !ok {
				// Channel was closed (session disconnected)
				r.mutex.RLock()
				currentQA := r.qaHistory[questionID]
				r.mutex.RUnlock()
				return currentQA, fmt.Errorf("answer channel closed - session disconnected")
			}
			return updatedQA, nil
		case <-time.After(24 * time.Hour): // Fallback timeout to prevent infinite hangs
			r.mutex.RLock()
			currentQA := r.qaHistory[questionID]
			r.mutex.RUnlock()
			return currentQA, fmt.Errorf("fallback timeout reached (24h)")
		}
	} else {
		// With timeout
		select {
		case updatedQA, ok := <-waiter:
			if !ok {
				// Channel was closed (session disconnected)
				r.mutex.RLock()
				currentQA := r.qaHistory[questionID]
				r.mutex.RUnlock()
				return currentQA, fmt.Errorf("answer channel closed - session disconnected")
			}
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

		for range ticker.C {
			r.cleanupExpiredEntries()
		}
	}()
}

// cleanupExpiredEntries removes Q&A entries that have expired
func (r *AgentQARegistry) cleanupExpiredEntries() {
	// Add panic recovery for cleanup operations
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in cleanupExpiredEntries", fmt.Sprintf("Panic: %v", rec))
		}
	}()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()
	expiredCount := 0

	for id, qa := range r.qaHistory {
		if now.After(qa.ExpiresAt) {
			// Clean up waiter channel if exists
			if waiter, exists := r.waiters[id]; exists {
				// Safely close the waiter channel
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							EmergencyLog("AgentQA", "Panic closing waiter channel", fmt.Sprintf("QuestionID: %s, Panic: %v", id, rec))
						}
					}()
					close(waiter)
				}()
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

// GetQAsByDirectory returns all Q&A entries for a specific directory, sorted by timestamp (newest first)
func (r *AgentQARegistry) GetQAsByDirectory(key string) []*QuestionAnswer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Get directory to check it exists
	dir := r.directories[key]
	if dir == nil {
		return []*QuestionAnswer{}
	}

	// Find all Q&As that belong to this directory
	qas := make([]*QuestionAnswer, 0)
	for _, qa := range r.qaHistory {
		if qa.DirectoryKey == key {
			qas = append(qas, qa)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(qas, func(i, j int) bool {
		return qas[i].Timestamp.After(qas[j].Timestamp)
	})

	return qas
}

// GetSystemHealth returns diagnostic information about the Q&A system
func (r *AgentQARegistry) GetSystemHealth() map[string]any {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	health := map[string]any{
		"directories_count":    len(r.directories),
		"qa_history_count":     len(r.qaHistory),
		"qa_queues_count":      len(r.qaQueues),
		"waiters_count":        len(r.waiters),
		"active_waiters_count": len(r.activeWaiters),
		"directories":          make([]map[string]any, 0),
		"active_waiters":       make([]map[string]any, 0),
	}

	// Add directory details
	for key, dir := range r.directories {
		dirInfo := map[string]any{
			"key":        key,
			"root_dir":   dir.RootDir,
			"specialty":  dir.Specialty,
			"created_at": dir.CreatedAt.Format(time.RFC3339),
			"has_queue":  false,
			"has_waiter": false,
		}

		// Check if queue exists
		if _, exists := r.qaQueues[key]; exists {
			dirInfo["has_queue"] = true
		}

		// Check if active waiter exists
		if waiter, exists := r.activeWaiters[key]; exists {
			dirInfo["has_waiter"] = true
			dirInfo["waiter_name"] = waiter.Name
			dirInfo["waiter_last_seen"] = waiter.LastSeen.Format(time.RFC3339)

			// Check if waiter context is still valid
			select {
			case <-waiter.Context.Done():
				dirInfo["waiter_context_cancelled"] = true
			default:
				dirInfo["waiter_context_cancelled"] = false
			}
		}

		health["directories"] = append(health["directories"].([]map[string]any), dirInfo)
	}

	// Add active waiter details
	for key, waiter := range r.activeWaiters {
		waiterInfo := map[string]any{
			"directory_key": key,
			"name":          waiter.Name,
			"last_seen":     waiter.LastSeen.Format(time.RFC3339),
			"channel_gen":   waiter.ChannelGen,
		}

		// Check if waiter context is still valid
		select {
		case <-waiter.Context.Done():
			waiterInfo["context_cancelled"] = true
		default:
			waiterInfo["context_cancelled"] = false
		}

		health["active_waiters"] = append(health["active_waiters"].([]map[string]any), waiterInfo)
	}

	return health
}

// startWaiterCleanupRoutine starts a goroutine that periodically cleans up expired active waiters
func (r *AgentQARegistry) startWaiterCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Run cleanup every hour
		defer ticker.Stop()

		for range ticker.C {
			r.cleanupExpiredWaiters()
		}
	}()
}

// cleanupExpiredWaiters removes expired active waiters
func (r *AgentQARegistry) cleanupExpiredWaiters() {
	// Add panic recovery for cleanup operations
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in cleanupExpiredWaiters", fmt.Sprintf("Panic: %v", rec))
		}
	}()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()
	expiredCount := 0
	cancelledCount := 0

	for id, waiter := range r.activeWaiters {
		shouldRemove := false
		reason := ""

		// Check if waiter is too old (not seen for 1 hour)
		if now.Sub(waiter.LastSeen) > 1*time.Hour {
			shouldRemove = true
			reason = "expired (not seen for 1 hour)"
			expiredCount++
		} else {
			// Check if waiter context is cancelled
			select {
			case <-waiter.Context.Done():
				shouldRemove = true
				reason = "context cancelled"
				cancelledCount++
			default:
				// Waiter is still active
			}
		}

		if shouldRemove {
			LogInfo("AgentQA", fmt.Sprintf("Cleaning up waiter '%s' in directory '%s': %s", waiter.Name, id, reason))

			// Clean up waiter channel if exists
			if waiter.Cancel != nil {
				waiter.Cancel()
			}

			// Remove from active waiters
			delete(r.activeWaiters, id)
		}
	}

	if expiredCount > 0 || cancelledCount > 0 {
		LogInfo("AgentQA", fmt.Sprintf("Cleaned up %d expired and %d cancelled active waiters", expiredCount, cancelledCount))
	}
}

// startHealthMonitoringRoutine starts a goroutine that periodically monitors system health
func (r *AgentQARegistry) startHealthMonitoringRoutine() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // Check health every 5 minutes
		defer ticker.Stop()

		for range ticker.C {
			r.checkSystemHealth()
		}
	}()
}

// checkSystemHealth performs health checks and logs warnings for problematic states
func (r *AgentQARegistry) checkSystemHealth() {
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in checkSystemHealth", fmt.Sprintf("Panic: %v", rec))
		}
	}()

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Check for directories without active waiters but with pending questions
	for dirKey, dir := range r.directories {
		_, hasActiveWaiter := r.activeWaiters[dirKey]

		// Count pending questions
		pendingCount := 0
		for _, qa := range r.qaHistory {
			if qa.DirectoryKey == dirKey && qa.Status == QAStatusPending {
				pendingCount++
			}
		}

		if !hasActiveWaiter && pendingCount > 0 {
			LogWarn("AgentQA", "Health Issue: Directory has pending questions but no active waiter",
				fmt.Sprintf("Directory: %s, Pending: %d, Specialty: %s", dirKey, pendingCount, dir.Specialty))
		}
	}

	// Check for active waiters with cancelled contexts
	cancelledWaiters := 0
	for dirKey, waiter := range r.activeWaiters {
		select {
		case <-waiter.Context.Done():
			cancelledWaiters++
			LogWarn("AgentQA", "Health Issue: Active waiter has cancelled context",
				fmt.Sprintf("Directory: %s, Waiter: %s", dirKey, waiter.Name))
		default:
			// Waiter context is still active
		}
	}

	// Log overall health summary
	if cancelledWaiters > 0 {
		LogWarn("AgentQA", "Health Summary",
			fmt.Sprintf("Directories: %d, Active Waiters: %d, Cancelled Waiters: %d",
				len(r.directories), len(r.activeWaiters), cancelledWaiters))
	}
}

// Global registry instance
var agentQARegistry = NewAgentQARegistry()
