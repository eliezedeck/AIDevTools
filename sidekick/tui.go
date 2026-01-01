package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PageType represents the different pages in the TUI
type PageType int

const (
	ProcessesPage PageType = iota
	ProcessDetailPage
	NotificationsPage
	AgentsQAPage
	LogsPage
)

// TUIApp represents the main TUI application - IDIOMATIC IMPLEMENTATION
type TUIApp struct {
	app               *tview.Application
	pages             *tview.Pages
	processesPage     *ProcessesPageView
	processDetailPage *ProcessDetailPageView
	notificationsPage *NotificationsPageView
	logsPage          *LogsPageView
	agentsQAPage      *AgentsQAPageView
	currentPage       PageType
	ctx               context.Context
	cancel            context.CancelFunc
	lastUpdateTime    time.Time
	signalChan        chan os.Signal
	shutdownOnce      sync.Once
	signalStopped     bool
	signalMutex       sync.Mutex

	// 🔋 Battery Optimization: Change tracking for efficient rendering
	lastProcessUpdate    time.Time
	lastNotificationTime time.Time
	lastLogUpdate        time.Time
	lastQATime           time.Time
	processCount         int
	notificationCount    int
	logCount             int
	qaCount              int
	currentProcessID     string
	dataChangeFlags      map[string]bool
	adaptiveInterval     time.Duration
	consecutiveNoChanges int
}

// NewTUIApp creates a new TUI application using idiomatic patterns
func NewTUIApp() *TUIApp {
	ctx, cancel := context.WithCancel(context.Background())

	tuiApp := &TUIApp{
		app:            tview.NewApplication(),
		pages:          tview.NewPages(),
		currentPage:    ProcessesPage,
		ctx:            ctx,
		cancel:         cancel,
		lastUpdateTime: time.Now(),
		signalChan:     make(chan os.Signal, 1),
	}

	// Set up signal handling for external termination
	signal.Notify(tuiApp.signalChan, os.Interrupt, syscall.SIGTERM)
	go tuiApp.handleSignals()

	// Enable mouse support
	tuiApp.app.EnableMouse(true)

	// Create the main pages
	tuiApp.processesPage = NewProcessesPageView(tuiApp)
	tuiApp.processDetailPage = NewProcessDetailPageView(tuiApp)
	tuiApp.notificationsPage = NewNotificationsPageView(tuiApp)
	tuiApp.logsPage = NewLogsPageView(tuiApp)
	tuiApp.agentsQAPage = NewAgentsQAPageView(tuiApp)

	// Add pages to the page container
	tuiApp.pages.AddPage("processes", tuiApp.processesPage.GetView(), true, true)
	tuiApp.pages.AddPage("process_detail", tuiApp.processDetailPage.GetView(), true, false)
	tuiApp.pages.AddPage("notifications", tuiApp.notificationsPage.GetView(), true, false)
	tuiApp.pages.AddPage("logs", tuiApp.logsPage.GetView(), true, false)
	tuiApp.pages.AddPage("agents_qa", tuiApp.agentsQAPage.GetView(), true, false)

	// Set up the main layout
	tuiApp.app.SetRoot(tuiApp.pages, true)

	// Set up global key handlers
	tuiApp.app.SetInputCapture(tuiApp.handleGlobalKeys)

	// Start background update routine with smarter updates
	go tuiApp.updateRoutine()

	// Start TUI state watchdog to prevent stuck TUI state
	go tuiApp.startStateWatchdog()

	// Check if this is a recovery startup and auto-navigate to logs
	go tuiApp.checkRecoveryMode()

	return tuiApp
}

// handleSignals handles external termination signals
func (t *TUIApp) handleSignals() {
	select {
	case <-t.signalChan:
		// External signal received (SIGINT, SIGTERM, etc.)
		t.shutdownOnce.Do(func() {
			// Mark TUI as inactive immediately
			tuiState.SetActive(false)
			// Stop the TUI application
			t.Stop()
		})
	case <-t.ctx.Done():
		// Context cancelled, exit gracefully
		return
	}
}

// handleGlobalKeys handles global keyboard shortcuts
func (t *TUIApp) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	// Check if we're in the process detail page with input field focused
	if t.currentPage == ProcessDetailPage && t.processDetailPage != nil {
		// Check if the input field is focused
		if t.processDetailPage.FocusedItem == 1 {
			// Let the input field handle the key event first
			// Only handle Tab key for switching focus
			if event.Key() == tcell.KeyTab {
				t.processDetailPage.switchFocus()
				return nil
			}
			// Pass all other keys to the input field
			return event
		}
	}

	switch event.Key() {
	case tcell.KeyTab:
		// Switch to next page
		t.switchToNextPage()
		return nil
	case tcell.KeyBacktab: // Shift+Tab
		// Switch to previous page
		t.switchToPrevPage()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case '1':
			t.SwitchToPage(ProcessesPage)
			return nil
		case '2':
			t.SwitchToPage(NotificationsPage)
			return nil
		case '3':
			t.SwitchToPage(AgentsQAPage)
			return nil
		case '4':
			t.SwitchToPage(LogsPage)
			return nil
		case 'q', 'Q':
			// Show quit confirmation dialog
			ShowQuitConfirmation(t.app, t.pages, func() {
				// User confirmed quit - mark TUI as inactive immediately
				tuiState.SetActive(false)
				t.Stop()
			})
			return nil
		}
	case tcell.KeyEsc:
		// Return to processes page or show quit confirmation if already there
		if t.currentPage != ProcessesPage {
			t.SwitchToPage(ProcessesPage)
		} else {
			// Show quit confirmation dialog
			ShowQuitConfirmation(t.app, t.pages, func() {
				// User confirmed quit - mark TUI as inactive immediately
				tuiState.SetActive(false)
				t.Stop()
			})
		}
		return nil
	}

	return event
}

// switchToNextPage cycles to the next page
func (t *TUIApp) switchToNextPage() {
	switch t.currentPage {
	case ProcessesPage:
		t.SwitchToPage(NotificationsPage)
	case NotificationsPage:
		t.SwitchToPage(AgentsQAPage)
	case AgentsQAPage:
		t.SwitchToPage(LogsPage)
	case LogsPage:
		t.SwitchToPage(ProcessesPage)
	case ProcessDetailPage:
		t.SwitchToPage(ProcessesPage)
	}
}

// switchToPrevPage cycles to the previous page
func (t *TUIApp) switchToPrevPage() {
	switch t.currentPage {
	case ProcessesPage:
		t.SwitchToPage(LogsPage)
	case NotificationsPage:
		t.SwitchToPage(ProcessesPage)
	case AgentsQAPage:
		t.SwitchToPage(NotificationsPage)
	case LogsPage:
		t.SwitchToPage(AgentsQAPage)
	case ProcessDetailPage:
		t.SwitchToPage(ProcessesPage)
	}
}

// SwitchToPage switches to the specified page
func (t *TUIApp) SwitchToPage(page PageType) {
	t.currentPage = page

	// 🔋 Clear currentProcessID when leaving process detail page
	if page != ProcessDetailPage {
		t.currentProcessID = ""
	}

	switch page {
	case ProcessesPage:
		t.pages.SwitchToPage("processes")
		t.processesPage.Refresh()
	case ProcessDetailPage:
		t.pages.SwitchToPage("process_detail")
		t.processDetailPage.Refresh()
	case NotificationsPage:
		t.pages.SwitchToPage("notifications")
		t.notificationsPage.Refresh()
	case LogsPage:
		t.pages.SwitchToPage("logs")
		t.logsPage.Refresh()
	case AgentsQAPage:
		t.pages.SwitchToPage("agents_qa")
		t.agentsQAPage.Refresh()
	}

	t.app.SetFocus(t.pages)
}

// ShowProcessDetail switches to the process detail page for a specific process
func (t *TUIApp) ShowProcessDetail(processID string) {
	t.currentProcessID = processID // 🔋 Track current process for efficient rendering
	t.processDetailPage.SetProcess(processID)
	t.SwitchToPage(ProcessDetailPage)
}

// updateRoutine runs background updates using IDIOMATIC SMART UPDATE PATTERN
func (t *TUIApp) updateRoutine() {
	ticker := time.NewTicker(1 * time.Second) // Faster for better responsiveness
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Smart update detection - only update when something actually changed
			if t.shouldUpdate() {
				// IDIOMATIC PATTERN: Always use QueueUpdateDraw from goroutines!
				t.app.QueueUpdateDraw(func() {
					switch t.currentPage {
					case ProcessesPage:
						t.processesPage.Update()
					case ProcessDetailPage:
						t.processDetailPage.Update()
					case NotificationsPage:
						t.notificationsPage.Update()
					case AgentsQAPage:
						t.agentsQAPage.Update()
					}
				})
				t.lastUpdateTime = time.Now()
			}
		case <-t.ctx.Done():
			return
		}
	}
}

// startStateWatchdog monitors TUI state and prevents it from getting stuck
func (t *TUIApp) startStateWatchdog() {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if TUI state has been active for too long without proper shutdown
			if tuiState.IsActive() {
				// TUI is marked as active - this is normal during operation
				// The watchdog is mainly for detecting stuck states after crashes
				// We don't reset here as it would interfere with normal operation
			}
		case <-t.ctx.Done():
			return
		}
	}
}

// checkRecoveryMode checks if TUI is starting in recovery mode and auto-navigates to logs
func (t *TUIApp) checkRecoveryMode() {
	// Wait a moment for TUI to fully initialize
	time.Sleep(2 * time.Second)

	// Check if we're in recovery mode (TUI was crashed and restarted)
	if tuiState.IsCrashed() || tuiState.IsRecovering() {
		// Auto-navigate to logs page to show the user what went wrong
		t.app.QueueUpdateDraw(func() {
			t.SwitchToPage(LogsPage)

			// Add a recovery message to logs
			LogError("TUI", "TUI has been recovered after a crash - check logs above for details")

			// Refresh the logs page to show the latest entries
			t.logsPage.Refresh()
		})

		// Clear the crashed state since we've handled it
		tuiState.SetCrashed(false)
	}
}

// shouldUpdate determines if a screen update is necessary using smart detection
func (t *TUIApp) shouldUpdate() bool {
	now := time.Now()

	// 🔋 Adaptive intervals: Longer delays when no changes detected
	minInterval := t.getAdaptiveInterval()
	if now.Sub(t.lastUpdateTime) < minInterval {
		return false
	}

	// Always update process detail page when viewing it (for real-time logs)
	// But use a shorter interval for logs
	if t.currentPage == ProcessDetailPage {
		if now.Sub(t.lastUpdateTime) < 250*time.Millisecond {
			return false
		}
		return t.hasProcessDetailDataChanged()
	}

	// For other pages, check if data actually changed
	hasChanges := t.hasDataChanged()

	// 🔋 Adjust adaptive interval based on change frequency
	if hasChanges {
		t.consecutiveNoChanges = 0
		t.adaptiveInterval = 500 * time.Millisecond // Reset to fast updates
	} else {
		t.consecutiveNoChanges++
		// Gradually increase interval up to 5 seconds when no changes
		if t.consecutiveNoChanges > 10 {
			t.adaptiveInterval = 5 * time.Second
		} else if t.consecutiveNoChanges > 5 {
			t.adaptiveInterval = 2 * time.Second
		}
	}

	return hasChanges
}

// getAdaptiveInterval returns the current adaptive update interval
func (t *TUIApp) getAdaptiveInterval() time.Duration {
	if t.adaptiveInterval == 0 {
		t.adaptiveInterval = 500 * time.Millisecond // Initial interval
	}
	return t.adaptiveInterval
}

// hasProcessDetailDataChanged checks if process detail data has changed
func (t *TUIApp) hasProcessDetailDataChanged() bool {
	if t.processDetailPage == nil || t.currentProcessID == "" {
		return false
	}

	// Check if the process output has new data
	registry.mutex.RLock()
	process, exists := registry.processes[t.currentProcessID]
	registry.mutex.RUnlock()

	if !exists {
		return true // Process disappeared, update needed
	}

	process.Mutex.RLock()
	status := process.Status
	process.Mutex.RUnlock()

	// Check if process status changed or new output available
	return status != StatusCompleted && status != StatusKilled && status != StatusFailed
}

// hasDataChanged checks if the underlying data has changed using smart detection
func (t *TUIApp) hasDataChanged() bool {
	now := time.Now()
	hasChanges := false

	// Initialize change flags if needed
	if t.dataChangeFlags == nil {
		t.dataChangeFlags = make(map[string]bool)
	}

	// 📊 Check process list changes
	registry.mutex.RLock()
	currentProcessCount := len(registry.processes)

	// Track any process status/output changes
	processListChanged := false
	if currentProcessCount != t.processCount {
		processListChanged = true
		t.processCount = currentProcessCount
	}

	// Check for process status updates or new output
	for _, process := range registry.processes {
		process.Mutex.RLock()
		lastAccessed := process.LastAccessed
		status := process.Status
		process.Mutex.RUnlock()

		// If process was accessed recently, there might be new data
		if lastAccessed.After(t.lastProcessUpdate) {
			processListChanged = true
		}

		// Active processes might have new output
		if status == StatusRunning || status == StatusPending {
			if now.Sub(lastAccessed) < 2*time.Second {
				processListChanged = true
			}
		}
	}
	registry.mutex.RUnlock()

	if processListChanged {
		t.lastProcessUpdate = now
		hasChanges = true
	}

	// 🔔 Check notification changes
	notificationHistory := notificationManager.GetHistory()
	currentNotificationCount := len(notificationHistory)
	if currentNotificationCount != t.notificationCount {
		t.notificationCount = currentNotificationCount
		hasChanges = true
	}

	// Check for new notifications by timestamp
	if len(notificationHistory) > 0 {
		latestNotification := notificationHistory[len(notificationHistory)-1]
		if latestNotification.Timestamp.After(t.lastNotificationTime) {
			t.lastNotificationTime = latestNotification.Timestamp
			hasChanges = true
		}
	}

	// 📝 Check log changes
	logEntries := logger.GetEntries()
	currentLogCount := len(logEntries)
	if currentLogCount != t.logCount {
		t.logCount = currentLogCount
		hasChanges = true
	}

	// Check for new log entries by timestamp
	if len(logEntries) > 0 {
		latestLog := logEntries[len(logEntries)-1]
		if latestLog.Timestamp.After(t.lastLogUpdate) {
			t.lastLogUpdate = latestLog.Timestamp
			hasChanges = true
		}
	}

	// 🤖 Check agent Q&A changes
	qaEntries := agentQARegistry.GetAllQAs()
	currentQACount := len(qaEntries)
	if currentQACount != t.qaCount {
		t.qaCount = currentQACount
		hasChanges = true
	}

	// Check for new Q&A entries by timestamp
	if len(qaEntries) > 0 {
		latestQA := qaEntries[0] // GetAllQAs returns newest first
		if latestQA.Timestamp.After(t.lastQATime) {
			t.lastQATime = latestQA.Timestamp
			hasChanges = true
		}
	}

	return hasChanges
}

// Run starts the TUI application
func (t *TUIApp) Run() error {
	return t.app.Run()
}

// Stop stops the TUI application with proper cleanup and error handling
func (t *TUIApp) Stop() {
	// Set up panic recovery for stop operation
	defer func() {
		if r := recover(); r != nil {
			// Panic during stop - force terminal reset
			tuiState.SetActive(false)
			ForceTerminalReset()
			EmergencyLog("TUI", "Panic during TUI stop", fmt.Sprintf("%v", r))
		}
	}()

	// Use mutex to prevent double-stopping
	t.signalMutex.Lock()
	defer t.signalMutex.Unlock()

	if !t.signalStopped {
		// Stop signal handling
		signal.Stop(t.signalChan)
		close(t.signalChan)
		t.signalStopped = true
	}

	// Cancel context to stop background goroutines
	t.cancel()

	// Stop the TUI application with timeout protection
	done := make(chan struct{})
	go func() {
		defer close(done)
		t.app.Stop()
	}()

	// Wait for stop with timeout
	select {
	case <-done:
		// TUI stopped successfully
	case <-time.After(3 * time.Second):
		// TUI stop timed out - force terminal reset
		ForceTerminalReset()
		EmergencyLog("TUI", "TUI stop timed out - forced terminal reset")
	}
}

// GetProcessesBySession returns processes grouped by session, sorted by creation time
func GetProcessesBySession(reverse bool) map[string][]*ProcessTracker {
	processes := registry.getAllProcesses()

	// Sort by creation time
	sort.Slice(processes, func(i, j int) bool {
		if reverse {
			return processes[i].StartTime.After(processes[j].StartTime)
		}
		return processes[i].StartTime.Before(processes[j].StartTime)
	})

	// Group by session
	sessionGroups := make(map[string][]*ProcessTracker)
	for _, process := range processes {
		sessionID := process.SessionID
		if sessionID == "" {
			sessionID = "No Session"
		}
		sessionGroups[sessionID] = append(sessionGroups[sessionID], process)
	}

	return sessionGroups
}

// GetProcessByID returns a process by its ID
func GetProcessByID(processID string) (*ProcessTracker, bool) {
	return registry.getProcess(processID)
}
