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
	// Note: QAStatusTimeout removed - questioner timeout doesn't change status
	// Status is specialist-only; questioners just get timeout errors
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
	DirectoryKey   string // The directory this question belongs to
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
	RootDir     string    // Project root directory (physical folder path)
	Specialty   string    // Area of expertise
	Instruction string    // Usage guidance
	CreatedAt   time.Time // When directory was created
}

// ActiveWaiter tracks an active specialist waiting for questions
type ActiveWaiter struct {
	Name     string
	Context  context.Context
	Cancel   context.CancelFunc
	LastSeen time.Time
}

// AgentQARegistry manages Q&A exchanges and specialist registrations
// Uses condition variables instead of channels to avoid race conditions
type AgentQARegistry struct {
	directories    map[string]*SpecialistDirectory // key: "<root-dir>-<specialty>"
	questionQueues map[string][]*QuestionAnswer    // key: "<root-dir>-<specialty>" (APPEND-ONLY queue/history)
	qaIndex        map[string]*QuestionAnswer      // key: Q&A ID (for fast lookup)
	activeWaiters  map[string]*ActiveWaiter        // key: "<root-dir>-<specialty>", tracks active specialists

	// Condition variables for notification (avoid channel lifecycle issues)
	dirConds    map[string]*sync.Cond // key: dirKey - wakes specialist when question arrives
	answerConds map[string]*sync.Cond // key: questionID - wakes questioner when answer arrives

	mutex sync.Mutex // Must be Mutex (not RWMutex) for sync.Cond
}

// NewAgentQARegistry creates a new agent Q&A registry
func NewAgentQARegistry() *AgentQARegistry {
	r := &AgentQARegistry{
		directories:    make(map[string]*SpecialistDirectory),
		questionQueues: make(map[string][]*QuestionAnswer),
		qaIndex:        make(map[string]*QuestionAnswer),
		activeWaiters:  make(map[string]*ActiveWaiter),
		dirConds:       make(map[string]*sync.Cond),
		answerConds:    make(map[string]*sync.Cond),
	}
	// Start unified maintenance routine
	r.startMaintenanceRoutine()
	return r
}

// getDirCond gets or creates a condition variable for a directory
func (r *AgentQARegistry) getDirCond(dirKey string) *sync.Cond {
	if r.dirConds[dirKey] == nil {
		r.dirConds[dirKey] = sync.NewCond(&r.mutex)
	}
	return r.dirConds[dirKey]
}

// getAnswerCond gets or creates a condition variable for a question
func (r *AgentQARegistry) getAnswerCond(questionID string) *sync.Cond {
	if r.answerConds[questionID] == nil {
		r.answerConds[questionID] = sync.NewCond(&r.mutex)
	}
	return r.answerConds[questionID]
}

// GetDirectoryBySpecialty returns the first directory for a given specialty
func (r *AgentQARegistry) GetDirectoryBySpecialty(specialty string) *SpecialistDirectory {
	r.mutex.Lock()
	defer r.mutex.Unlock()

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
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.directories[key]
}

// ListDirectories returns all directories
func (r *AgentQARegistry) ListDirectories() []*SpecialistDirectory {
	r.mutex.Lock()
	defer r.mutex.Unlock()

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

// askQuestionInternal is the core implementation for submitting questions to specialists.
// If wait is true, blocks until answer is available (respecting timeout).
// If wait is false, returns immediately with the question ID.
// Questions are queued even if no specialist is currently waiting - a specialist can pick it up later.
func (r *AgentQARegistry) askQuestionInternal(from, specialty, rootDir, question string, wait bool, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.Lock()

	// 1. Create directory key
	dirKey := fmt.Sprintf("%s-%s", rootDir, specialty)

	// 2. Create or get directory
	if r.directories[dirKey] == nil {
		r.directories[dirKey] = &SpecialistDirectory{
			Key:       dirKey,
			RootDir:   rootDir,
			Specialty: specialty,
			CreatedAt: time.Now(),
		}
		LogInfo("AgentQA", fmt.Sprintf("Created directory '%s' for incoming question", dirKey))
	}

	// 3. Initialize question queue for directory if needed
	if r.questionQueues[dirKey] == nil {
		r.questionQueues[dirKey] = make([]*QuestionAnswer, 0)
	}

	// 4. Create question entry
	qa := &QuestionAnswer{
		ID:           uuid.New().String(),
		From:         from,
		To:           specialty, // Will be updated by specialist who picks it up
		Question:     question,
		Status:       QAStatusPending,
		Timestamp:    time.Now(),
		DirectoryKey: dirKey,
	}

	// 5. Add to index for fast lookup
	r.qaIndex[qa.ID] = qa

	// 6. Append to queue (NEVER removed - append-only)
	r.questionQueues[dirKey] = append(r.questionQueues[dirKey], qa)

	// 7. Wake up specialist waiting for THIS directory only
	dirCond := r.getDirCond(dirKey)
	dirCond.Signal() // Signal, not Broadcast - only one specialist per directory

	// Log whether there's an active waiter
	if waiter, exists := r.activeWaiters[dirKey]; exists {
		// Check if context is still valid
		select {
		case <-waiter.Context.Done():
			LogInfo("AgentQA", fmt.Sprintf("Question %s queued in directory '%s' (waiter context cancelled)", qa.ID, dirKey))
		default:
			LogInfo("AgentQA", fmt.Sprintf("Question %s sent to directory '%s' (active waiter: %s)", qa.ID, dirKey, waiter.Name))
		}
	} else {
		LogInfo("AgentQA", fmt.Sprintf("Question %s queued in directory '%s' (no active waiter yet)", qa.ID, dirKey))
	}

	r.mutex.Unlock()

	// 8. If not waiting, return immediately
	if !wait {
		return qa, nil
	}

	// 9. Wait for answer
	return r.waitForAnswer(qa.ID, timeout)
}

// waitForAnswer polls for an answer using condition variables
// Questioners should prefer NO timeout (timeout=0). If timeout is set, it only
// affects how long we wait - NOT the question status.
// Question status is ONLY changed by the specialist (Completed/Failed).
func (r *AgentQARegistry) waitForAnswer(questionID string, timeout time.Duration) (*QuestionAnswer, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	qa := r.qaIndex[questionID]
	if qa == nil {
		return nil, fmt.Errorf("question ID '%s' not found", questionID)
	}

	// Get or create condition variable for this question
	answerCond := r.getAnswerCond(questionID)

	// Calculate deadline if timeout is set
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	// Start timeout watcher if needed (ONCE per call)
	// Note: This goroutine is acceptable because questioners should prefer NO timeout.
	if timeout > 0 {
		done := make(chan struct{})
		defer close(done)

		go func() {
			select {
			case <-time.After(timeout):
				r.mutex.Lock()
				answerCond.Broadcast() // Wake to check timeout
				r.mutex.Unlock()
			case <-done:
				// Clean exit
			}
		}()
	}

	// Main wait loop
	for {
		qa = r.qaIndex[questionID]
		if qa == nil {
			return nil, fmt.Errorf("question ID '%s' disappeared", questionID)
		}

		// Check if answered (only specialist can set these statuses)
		if qa.Status == QAStatusCompleted || qa.Status == QAStatusFailed {
			return qa, nil
		}

		// Check timeout - DO NOT modify qa.Status!
		// Status is specialist-only; we just return an error to the caller
		if timeout > 0 && time.Now().After(deadline) {
			// Return current state with timeout error
			// Questioner can call GetAnswer later to retrieve late answer
			return qa, fmt.Errorf("timeout waiting for answer")
		}

		// Wait for notification (releases lock, reacquires on wake)
		answerCond.Wait()
	}
}

// AskQuestion submits a question to a specialist directory and waits for a response
func (r *AgentQARegistry) AskQuestion(from, specialty, rootDir, question string, timeout time.Duration) (*QuestionAnswer, error) {
	return r.askQuestionInternal(from, specialty, rootDir, question, true, timeout)
}

// WaitForQuestion waits for a question for a specialist (blocking)
func (r *AgentQARegistry) WaitForQuestion(name, specialty, rootDir, instructions string, timeout time.Duration) (*QuestionAnswer, error) {
	return r.WaitForQuestionWithContext(context.Background(), name, specialty, rootDir, instructions, timeout)
}

// WaitForQuestionWithContext waits for a question for a specialist with context cancellation support
func (r *AgentQARegistry) WaitForQuestionWithContext(ctx context.Context, name, specialty, rootDir, instructions string, timeout time.Duration) (*QuestionAnswer, error) {
	dirKey := fmt.Sprintf("%s-%s", rootDir, specialty)

	r.mutex.Lock()

	// 1. Check for existing waiter - ALLOW SAME SPECIALIST TO RE-ENTER
	if existingWaiter, exists := r.activeWaiters[dirKey]; exists {
		// Check if existing waiter's context is cancelled
		existingContextCancelled := false
		select {
		case <-existingWaiter.Context.Done():
			existingContextCancelled = true
		default:
		}

		if existingWaiter.Name == name {
			// Same specialist re-entering - ALWAYS update context to new HTTP request context
			// The old context may be cancelled or about to be cancelled by the HTTP transport
			LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' re-entering wait for directory '%s', updating context", name, dirKey))
			if existingWaiter.Cancel != nil {
				existingWaiter.Cancel() // Cancel old context
			}
			// NOTE: Do NOT call recoverOrphanedQuestions here - same specialist may still
			// be working on a question. Orphan recovery only happens when a DIFFERENT
			// specialist takes over or during maintenance cleanup.
			// Update with new context from current HTTP request
			waiterCtx, waiterCancel := context.WithCancel(ctx)
			existingWaiter.Context = waiterCtx
			existingWaiter.Cancel = waiterCancel
			existingWaiter.LastSeen = time.Now()
			// Fall through to wait loop (will use updated waiter)
		} else {
			// Different specialist
			if existingContextCancelled {
				// Old specialist is gone - clean up and recover orphans
				LogInfo("AgentQA", fmt.Sprintf("Cleaning up cancelled waiter '%s' for directory '%s'", existingWaiter.Name, dirKey))
				r.recoverOrphanedQuestions(dirKey, existingWaiter.Name)
				if existingWaiter.Cancel != nil {
					existingWaiter.Cancel()
				}
				delete(r.activeWaiters, dirKey)
				// Fall through to register new waiter
			} else {
				// Different specialist still active - reject
				r.mutex.Unlock()
				return nil, fmt.Errorf("another specialist '%s' is already waiting for questions in directory '%s'", existingWaiter.Name, dirKey)
			}
		}
	}

	// 2. Create or update directory
	if r.directories[dirKey] == nil {
		r.directories[dirKey] = &SpecialistDirectory{
			Key:         dirKey,
			RootDir:     rootDir,
			Specialty:   specialty,
			Instruction: instructions,
			CreatedAt:   time.Now(),
		}
		LogInfo("AgentQA", fmt.Sprintf("Created new directory '%s'", dirKey))
	} else if instructions != "" {
		r.directories[dirKey].Instruction = instructions
	}

	// 3. Initialize question queue if needed
	if r.questionQueues[dirKey] == nil {
		r.questionQueues[dirKey] = make([]*QuestionAnswer, 0)
	}

	// 4. Register as active waiter (only if not already registered as same name)
	waiter := r.activeWaiters[dirKey]
	if waiter == nil || waiter.Name != name {
		waiterCtx, waiterCancel := context.WithCancel(ctx)
		waiter = &ActiveWaiter{
			Name:     name,
			Context:  waiterCtx,
			Cancel:   waiterCancel,
			LastSeen: time.Now(),
		}
		r.activeWaiters[dirKey] = waiter
		LogInfo("AgentQA", fmt.Sprintf("Registered specialist '%s' for directory '%s'", name, dirKey))
	}

	// IMPORTANT: Capture the context for THIS specific call
	// When re-entering, waiter.Context may be updated by a newer call
	// Each call must watch its own context to avoid goroutine leaks
	myCtx := waiter.Context

	// 5. Get per-directory condition variable
	dirCond := r.getDirCond(dirKey)

	// Calculate deadline if timeout is set
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	// 6. Start context cancellation watcher (ONCE, not per loop)
	// Watch THIS call's context (myCtx), not the shared waiter.Context
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-myCtx.Done():
			r.mutex.Lock()
			dirCond.Broadcast() // Wake to check cancellation
			r.mutex.Unlock()
		case <-done:
			// Clean exit
		}
	}()

	// 7. Start timeout watcher if needed (ONCE, not per loop)
	if timeout > 0 {
		go func() {
			select {
			case <-time.After(timeout):
				r.mutex.Lock()
				dirCond.Broadcast() // Wake to check timeout
				r.mutex.Unlock()
			case <-done:
				// Clean exit
			}
		}()
	}

	LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' waiting for questions in directory '%s'", name, dirKey))

	// 8. Main wait loop
	for {
		// Update LastSeen only if we're still the active waiter
		// (a newer call may have replaced us with a new context)
		currentWaiter := r.activeWaiters[dirKey]
		if currentWaiter != nil && currentWaiter.Context == myCtx {
			currentWaiter.LastSeen = time.Now()
		}

		// CRITICAL: Check if this specialist already has a question in Processing status
		// Enforce one in-flight question per specialist rule
		hasInFlightQuestion := false
		for _, qa := range r.questionQueues[dirKey] {
			if qa.Status == QAStatusProcessing && qa.To == name {
				hasInFlightQuestion = true
				break
			}
		}

		// Scan for first Pending question (FIFO order) only if no in-flight question
		// Queue is APPEND-ONLY - never remove entries!
		var foundQuestion *QuestionAnswer
		if !hasInFlightQuestion {
			for _, qa := range r.questionQueues[dirKey] {
				if qa.Status == QAStatusPending {
					// Take this question (mark as Processing, don't remove from queue)
					qa.Status = QAStatusProcessing
					qa.To = name
					foundQuestion = qa
					break // FIFO: take earliest pending
				}
			}
		}

		if foundQuestion != nil {
			// DON'T delete activeWaiter - specialist will call again after answering
			r.mutex.Unlock()
			LogInfo("AgentQA", fmt.Sprintf("Question %s assigned to specialist '%s'", foundQuestion.ID, name))
			return foundQuestion, nil
		}

		// Check context cancellation (check THIS call's context, not waiter.Context)
		select {
		case <-myCtx.Done():
			// Context cancelled - only clean up if we're still the active waiter
			// A newer call may have replaced us with a new context
			currentWaiter := r.activeWaiters[dirKey]
			if currentWaiter != nil && currentWaiter.Context == myCtx {
				// We're still the active waiter - clean up
				delete(r.activeWaiters, dirKey)
				LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' context cancelled in directory '%s'", name, dirKey))
			} else {
				// A newer call has taken over - just exit quietly
				LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' old context cancelled, newer call active in directory '%s'", name, dirKey))
			}
			r.mutex.Unlock()
			return nil, fmt.Errorf("context cancelled: %w", myCtx.Err())
		default:
			// Context still valid
		}

		// Check timeout
		if timeout > 0 && time.Now().After(deadline) {
			// DON'T delete activeWaiter on timeout - same specialist expected to retry
			// This intentionally blocks a DIFFERENT specialist from registering
			// until either (a) same specialist reconnects, or (b) maintenance cleans up
			r.mutex.Unlock()
			LogInfo("AgentQA", fmt.Sprintf("Specialist '%s' timed out waiting in directory '%s'", name, dirKey))
			return nil, fmt.Errorf("timeout waiting for question")
		}

		// Wait for notification on THIS directory's cond (releases and reacquires mutex)
		dirCond.Wait()
	}
}

// recoverOrphanedQuestions resets Processing questions back to Pending
// Called while holding mutex
func (r *AgentQARegistry) recoverOrphanedQuestions(dirKey, previousSpecialistName string) {
	recoveredCount := 0
	for _, qa := range r.questionQueues[dirKey] {
		if qa.Status == QAStatusProcessing && qa.To == previousSpecialistName {
			// Reset to pending - DO NOT re-enqueue (it's already in the queue)
			qa.Status = QAStatusPending
			qa.To = ""
			recoveredCount++
			LogInfo("AgentQA", fmt.Sprintf("Recovered orphaned question %s for directory '%s'", qa.ID, dirKey))
		}
	}
	if recoveredCount > 0 {
		LogInfo("AgentQA", fmt.Sprintf("Recovered %d orphaned questions in directory '%s'", recoveredCount, dirKey))
	}
}

// AnswerQuestion provides an answer to a question. A question can only be answered once and only once.
func (r *AgentQARegistry) AnswerQuestion(questionID, answer string, err error) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Get the Q&A entry
	qa, exists := r.qaIndex[questionID]
	if !exists {
		return fmt.Errorf("question ID '%s' not found", questionID)
	}

	// Only allow answering Pending or Processing questions
	// Note: There's no Timeout status anymore - questioner timeout doesn't change status
	if qa.Status == QAStatusCompleted {
		return fmt.Errorf("question ID '%s' has already been answered", questionID)
	}
	if qa.Status == QAStatusFailed {
		return fmt.Errorf("question ID '%s' has already failed and cannot be answered", questionID)
	}

	// Update state (only specialist can change status)
	qa.ProcessingTime = time.Since(qa.Timestamp)

	if err != nil {
		qa.Status = QAStatusFailed
		qa.Error = err.Error()
	} else {
		qa.Status = QAStatusCompleted
		qa.Answer = answer
		qa.Error = "" // Clear any previous error
	}

	// Wake up ALL questioners waiting for THIS answer
	// (Use Broadcast because multiple GetAnswer calls may be waiting on same question)
	if answerCond := r.answerConds[questionID]; answerCond != nil {
		answerCond.Broadcast()
	}

	LogInfo("AgentQA", fmt.Sprintf("Question %s answered by '%s'", questionID, qa.To))

	return nil
}

// GetQA returns a specific Q&A entry
func (r *AgentQARegistry) GetQA(id string) *QuestionAnswer {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.qaIndex[id]
}

// GetAllQAs returns all Q&A entries sorted by timestamp (newest first)
func (r *AgentQARegistry) GetAllQAs() []*QuestionAnswer {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	qas := make([]*QuestionAnswer, 0, len(r.qaIndex))
	for _, qa := range r.qaIndex {
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
	return r.askQuestionInternal(from, specialty, rootDir, question, false, 0)
}

// GetAnswer retrieves the answer for a previously asked question
// Same as waitForAnswer - just poll the state
func (r *AgentQARegistry) GetAnswer(questionID string, timeout time.Duration) (*QuestionAnswer, error) {
	return r.waitForAnswer(questionID, timeout)
}

// GetQAsByDirectory returns all Q&A entries for a specific directory, sorted by timestamp (newest first)
func (r *AgentQARegistry) GetQAsByDirectory(key string) []*QuestionAnswer {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Get directory to check it exists
	dir := r.directories[key]
	if dir == nil {
		return []*QuestionAnswer{}
	}

	// Get all Q&As from the question queue for this directory
	qas := make([]*QuestionAnswer, 0)
	if queue := r.questionQueues[key]; queue != nil {
		qas = append(qas, queue...)
	}

	// Sort by timestamp (newest first)
	sort.Slice(qas, func(i, j int) bool {
		return qas[i].Timestamp.After(qas[j].Timestamp)
	})

	return qas
}

// GetSystemHealth returns diagnostic information about the Q&A system
func (r *AgentQARegistry) GetSystemHealth() map[string]any {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Count total questions across all queues
	totalQuestions := 0
	for _, queue := range r.questionQueues {
		totalQuestions += len(queue)
	}

	health := map[string]any{
		"directories_count":     len(r.directories),
		"question_queues_count": len(r.questionQueues),
		"total_questions":       totalQuestions,
		"qa_index_count":        len(r.qaIndex),
		"active_waiters_count":  len(r.activeWaiters),
		"dir_conds_count":       len(r.dirConds),
		"answer_conds_count":    len(r.answerConds),
		"directories":           make([]map[string]any, 0),
		"active_waiters":        make([]map[string]any, 0),
	}

	// Add directory details
	for key, dir := range r.directories {
		dirInfo := map[string]any{
			"key":            key,
			"project_folder": dir.RootDir,
			"specialty":      dir.Specialty,
			"created_at":     dir.CreatedAt.Format(time.RFC3339),
			"has_queue":      false,
			"queue_size":     0,
			"has_waiter":     false,
		}

		// Check if queue exists and count
		if queue, exists := r.questionQueues[key]; exists {
			dirInfo["has_queue"] = true
			dirInfo["queue_size"] = len(queue)

			// Count by status
			pending := 0
			processing := 0
			completed := 0
			failed := 0
			for _, qa := range queue {
				switch qa.Status {
				case QAStatusPending:
					pending++
				case QAStatusProcessing:
					processing++
				case QAStatusCompleted:
					completed++
				case QAStatusFailed:
					failed++
				}
			}
			dirInfo["pending_count"] = pending
			dirInfo["processing_count"] = processing
			dirInfo["completed_count"] = completed
			dirInfo["failed_count"] = failed
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

// startMaintenanceRoutine starts a unified goroutine that handles all periodic maintenance tasks:
// - Health monitoring (every 5 minutes)
// - Stale waiter cleanup (every hour)
// Note: Questions stay in memory forever (append-only design)
func (r *AgentQARegistry) startMaintenanceRoutine() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		tickCount := 0
		for range ticker.C {
			tickCount++

			// Health check runs every tick (5 minutes)
			r.checkSystemHealth()

			// Cleanup tasks run every 12 ticks (1 hour)
			if tickCount%12 == 0 {
				r.cleanupStaleWaiters()
			}
		}
	}()
}

// cleanupStaleWaiters removes stale active waiters
// Note: Questions are NOT cleaned up - they stay in memory forever (append-only design)
func (r *AgentQARegistry) cleanupStaleWaiters() {
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in cleanupStaleWaiters", fmt.Sprintf("Panic: %v", rec))
		}
	}()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()
	expiredCount := 0
	cancelledCount := 0

	for dirKey, waiter := range r.activeWaiters {
		shouldRemove := false
		reason := ""

		// Check if waiter context is cancelled
		select {
		case <-waiter.Context.Done():
			shouldRemove = true
			reason = "context cancelled"
			cancelledCount++
			r.recoverOrphanedQuestions(dirKey, waiter.Name)
		default:
			// Check if waiter is too old (not seen for 1 hour)
			if now.Sub(waiter.LastSeen) > 1*time.Hour {
				shouldRemove = true
				reason = "expired (not seen for 1 hour)"
				expiredCount++
				if waiter.Cancel != nil {
					waiter.Cancel()
				}
				r.recoverOrphanedQuestions(dirKey, waiter.Name)
			}
		}

		if shouldRemove {
			LogInfo("AgentQA", fmt.Sprintf("Cleaning up waiter '%s' in directory '%s': %s", waiter.Name, dirKey, reason))
			delete(r.activeWaiters, dirKey)
		}
	}

	if expiredCount > 0 || cancelledCount > 0 {
		LogInfo("AgentQA", fmt.Sprintf("Cleaned up %d expired and %d cancelled active waiters", expiredCount, cancelledCount))
	}
}

// checkSystemHealth performs health checks and logs warnings for problematic states
func (r *AgentQARegistry) checkSystemHealth() {
	defer func() {
		if rec := recover(); rec != nil {
			EmergencyLog("AgentQA", "Panic in checkSystemHealth", fmt.Sprintf("Panic: %v", rec))
		}
	}()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()

	// Check for directories without active waiters but with pending questions
	for dirKey, dir := range r.directories {
		_, hasActiveWaiter := r.activeWaiters[dirKey]

		// Count pending questions
		pendingCount := 0
		if queue := r.questionQueues[dirKey]; queue != nil {
			for _, qa := range queue {
				if qa.Status == QAStatusPending {
					pendingCount++
				}
			}
		}

		if !hasActiveWaiter && pendingCount > 0 {
			LogWarn("AgentQA", "Health Issue: Directory has pending questions but no active waiter",
				fmt.Sprintf("Directory: %s, Pending: %d, Specialty: %s", dirKey, pendingCount, dir.Specialty))
		}
	}

	// Check for questions stuck in Processing status for too long
	stuckProcessingThreshold := 15 * time.Minute
	stuckQuestions := 0

	for _, qa := range r.qaIndex {
		if qa.Status == QAStatusProcessing {
			processingDuration := now.Sub(qa.Timestamp)
			if processingDuration > stuckProcessingThreshold {
				stuckQuestions++
				LogWarn("AgentQA", "Health Issue: Question stuck in Processing status",
					fmt.Sprintf("Question: %s, Directory: %s, Specialist: %s, Duration: %v",
						qa.ID, qa.DirectoryKey, qa.To, processingDuration))

				// Check if the specialist is still active
				if waiter, exists := r.activeWaiters[qa.DirectoryKey]; exists && waiter.Name == qa.To {
					// Specialist is still registered but hasn't answered
					LogWarn("AgentQA", "Specialist still active but not responding",
						fmt.Sprintf("Specialist: %s, Last seen: %v ago", qa.To, now.Sub(waiter.LastSeen)))
				} else {
					// Specialist is gone, this question is orphaned
					LogWarn("AgentQA", "Question is orphaned - specialist no longer active",
						fmt.Sprintf("Question: %s, Missing specialist: %s", qa.ID, qa.To))
				}
			}
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

	// Log overall health summary only if there are issues
	if cancelledWaiters > 0 || stuckQuestions > 0 {
		LogWarn("AgentQA", "Health Summary",
			fmt.Sprintf("Directories: %d, Active Waiters: %d, Cancelled Waiters: %d, Stuck Questions: %d",
				len(r.directories), len(r.activeWaiters), cancelledWaiters, stuckQuestions))
	}
}

// Global registry instance
var agentQARegistry = NewAgentQARegistry()
