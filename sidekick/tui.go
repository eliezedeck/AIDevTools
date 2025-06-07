package main

import (
	"context"
	"sort"
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
)

// TUIApp represents the main TUI application
type TUIApp struct {
	app              *tview.Application
	pages            *tview.Pages
	processesPage    *ProcessesPageView
	processDetailPage *ProcessDetailPageView
	notificationsPage *NotificationsPageView
	currentPage      PageType
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewTUIApp creates a new TUI application
func NewTUIApp() *TUIApp {
	ctx, cancel := context.WithCancel(context.Background())
	
	tuiApp := &TUIApp{
		app:         tview.NewApplication(),
		pages:       tview.NewPages(),
		currentPage: ProcessesPage,
		ctx:         ctx,
		cancel:      cancel,
	}
	
	// Enable mouse support
	tuiApp.app.EnableMouse(true)
	
	// Create the three main pages
	tuiApp.processesPage = NewProcessesPageView(tuiApp)
	tuiApp.processDetailPage = NewProcessDetailPageView(tuiApp)
	tuiApp.notificationsPage = NewNotificationsPageView(tuiApp)
	
	// Add pages to the page container
	tuiApp.pages.AddPage("processes", tuiApp.processesPage.GetView(), true, true)
	tuiApp.pages.AddPage("process_detail", tuiApp.processDetailPage.GetView(), true, false)
	tuiApp.pages.AddPage("notifications", tuiApp.notificationsPage.GetView(), true, false)
	
	// Set up the main layout
	tuiApp.app.SetRoot(tuiApp.pages, true)
	
	// Set up global key handlers
	tuiApp.app.SetInputCapture(tuiApp.handleGlobalKeys)
	
	// Start background update routine
	go tuiApp.updateRoutine()
	
	return tuiApp
}

// handleGlobalKeys handles global keyboard shortcuts
func (t *TUIApp) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
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
		case 'q', 'Q':
			// Quit application
			t.Stop()
			return nil
		}
	case tcell.KeyEsc:
		// Return to processes page or quit if already there
		if t.currentPage != ProcessesPage {
			t.SwitchToPage(ProcessesPage)
		} else {
			t.Stop()
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
		t.SwitchToPage(ProcessesPage)
	case ProcessDetailPage:
		t.SwitchToPage(ProcessesPage)
	}
}

// switchToPrevPage cycles to the previous page
func (t *TUIApp) switchToPrevPage() {
	switch t.currentPage {
	case ProcessesPage:
		t.SwitchToPage(NotificationsPage)
	case NotificationsPage:
		t.SwitchToPage(ProcessesPage)
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
	}
	
	t.app.SetFocus(t.pages)
}

// ShowProcessDetail switches to the process detail page for a specific process
func (t *TUIApp) ShowProcessDetail(processID string) {
	t.processDetailPage.SetProcess(processID)
	t.SwitchToPage(ProcessDetailPage)
}

// updateRoutine runs background updates for real-time data
func (t *TUIApp) updateRoutine() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Update the current page
			t.app.QueueUpdateDraw(func() {
				switch t.currentPage {
				case ProcessesPage:
					t.processesPage.Update()
				case ProcessDetailPage:
					t.processDetailPage.Update()
				case NotificationsPage:
					t.notificationsPage.Update()
				}
			})
		case <-t.ctx.Done():
			return
		}
	}
}

// Run starts the TUI application
func (t *TUIApp) Run() error {
	return t.app.Run()
}

// Stop stops the TUI application
func (t *TUIApp) Stop() {
	t.cancel()
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