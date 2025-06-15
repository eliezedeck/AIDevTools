package main

import (
	"context"
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
	LogsPage
	AgentsQAPage
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

	return tuiApp
}

// handleSignals handles external termination signals
func (t *TUIApp) handleSignals() {
	select {
	case <-t.signalChan:
		// External signal received (SIGINT, SIGTERM, etc.)
		t.shutdownOnce.Do(func() {
			// Mark TUI as inactive immediately
			setTUIActive(false)
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
			t.SwitchToPage(LogsPage)
			return nil
		case '4':
			t.SwitchToPage(AgentsQAPage)
			return nil
		case 'q', 'Q':
			// Show quit confirmation dialog
			ShowQuitConfirmation(t.app, t.pages, func() {
				// User confirmed quit - mark TUI as inactive immediately
				setTUIActive(false)
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
				setTUIActive(false)
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
		t.SwitchToPage(LogsPage)
	case LogsPage:
		t.SwitchToPage(AgentsQAPage)
	case AgentsQAPage:
		t.SwitchToPage(ProcessesPage)
	case ProcessDetailPage:
		t.SwitchToPage(ProcessesPage)
	}
}

// switchToPrevPage cycles to the previous page
func (t *TUIApp) switchToPrevPage() {
	switch t.currentPage {
	case ProcessesPage:
		t.SwitchToPage(AgentsQAPage)
	case NotificationsPage:
		t.SwitchToPage(ProcessesPage)
	case LogsPage:
		t.SwitchToPage(NotificationsPage)
	case AgentsQAPage:
		t.SwitchToPage(LogsPage)
	case ProcessDetailPage:
		t.SwitchToPage(ProcessesPage)
	}
}

// SwitchToPage switches to the specified page
func (t *TUIApp) SwitchToPage(page PageType) {
	t.currentPage = page

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

// shouldUpdate determines if a screen update is necessary using smart detection
func (t *TUIApp) shouldUpdate() bool {
	// Check if enough time has passed for rate limiting
	if time.Since(t.lastUpdateTime) < 500*time.Millisecond {
		return false
	}

	// Always update process detail page when viewing it (for real-time logs)
	if t.currentPage == ProcessDetailPage {
		return true
	}

	// For other pages, check if data actually changed
	return t.hasDataChanged()
}

// hasDataChanged checks if the underlying data has changed
func (t *TUIApp) hasDataChanged() bool {
	// This is a simplified check - in practice you could track modification times
	// For now, we'll update less frequently but still catch changes
	return true
}

// Run starts the TUI application
func (t *TUIApp) Run() error {
	return t.app.Run()
}

// Stop stops the TUI application
func (t *TUIApp) Stop() {
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

	// Stop the TUI application
	t.app.Stop()
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
