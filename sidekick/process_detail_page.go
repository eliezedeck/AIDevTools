package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ProcessDetailPageView represents the process detail page - IDIOMATIC IMPLEMENTATION
type ProcessDetailPageView struct {
	tuiApp         *TUIApp
	view           *tview.Flex
	infoPanel      *tview.TextView
	logView        *tview.TextView
	inputField     *tview.InputField
	statusBar      *tview.TextView
	processID      string
	autoScroll     bool
	FocusedItem    int // 0: log view, 1: input field
	lastLogContent string // Cache for incremental updates
	isScrolling    bool // Track if user is actively scrolling
	lastScrollTime time.Time // Track when user last scrolled
	scrollLockDuration time.Duration // How long to lock updates after scroll
}

// NewProcessDetailPageView creates a new process detail page view
func NewProcessDetailPageView(tuiApp *TUIApp) *ProcessDetailPageView {
	p := &ProcessDetailPageView{
		tuiApp:         tuiApp,
		infoPanel:      tview.NewTextView(),
		logView:        tview.NewTextView(),
		inputField:     tview.NewInputField(),
		statusBar:      tview.NewTextView(),
		autoScroll:     true,
		FocusedItem:    0,
		lastLogContent: "",
		isScrolling:    false,
		lastScrollTime: time.Now(),
		scrollLockDuration: 3 * time.Second, // Lock updates for 3 seconds after scroll
	}
	
	p.setupInfoPanel()
	p.setupLogView()
	p.setupInputField()
	p.setupStatusBar()
	p.setupLayout()
	
	// Initialize scroll status display
	p.updateScrollStatus()
	
	return p
}

// setupInfoPanel configures the process info panel
func (p *ProcessDetailPageView) setupInfoPanel() {
	p.infoPanel.SetBorder(true).SetTitle(" Process Info ").SetTitleAlign(tview.AlignLeft)
	p.infoPanel.SetDynamicColors(true)
	p.infoPanel.SetTextAlign(tview.AlignLeft)
}

// setupLogView configures the log viewer with IDIOMATIC PATTERNS
func (p *ProcessDetailPageView) setupLogView() {
	p.logView.SetBorder(true).SetTitle(" Logs ").SetTitleAlign(tview.AlignLeft)
	p.logView.SetDynamicColors(true)
	p.logView.SetScrollable(true)
	p.logView.SetInputCapture(p.handleLogViewKeys)
	// Enable word wrap for better log viewing
	p.logView.SetWordWrap(true)
	// Remove SetChangedFunc to prevent auto-scroll conflicts
	// Auto-scroll will be handled manually in update methods
}

// setupInputField configures the input field for stdin
func (p *ProcessDetailPageView) setupInputField() {
	p.inputField.SetBorder(true).SetTitle(" Send Input (Press Enter to send) ").SetTitleAlign(tview.AlignLeft)
	p.inputField.SetFieldBackgroundColor(tcell.ColorDarkBlue)
	p.inputField.SetDoneFunc(p.handleInputSubmit)
	p.inputField.SetInputCapture(p.handleInputFieldKeys)
}

// setupStatusBar configures the status bar
func (p *ProcessDetailPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]Tab[white]: Switch Focus | [yellow]Enter[white]: Send Input | [yellow]S[white]: Toggle Auto-scroll | [yellow]Esc[white]: Back | [yellow]Q[white]: Quit")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *ProcessDetailPageView) setupLayout() {
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.infoPanel, 7, 0, false).
		AddItem(p.logView, 0, 1, true).
		AddItem(p.inputField, 3, 0, false).
		AddItem(p.statusBar, 3, 0, false)
	
	// Set up global key handlers for the main view
	p.view.SetInputCapture(p.handleGlobalKeys)
	
	// Set up mouse handler for the log view to detect scroll events
	p.logView.SetMouseCapture(p.handleLogViewMouse)
}

// handleGlobalKeys handles global key events for this page
func (p *ProcessDetailPageView) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 's', 'S':
			p.toggleAutoScroll()
			return nil
		}
	}
	return event
}

// handleLogViewKeys handles key events for the log view
func (p *ProcessDetailPageView) handleLogViewKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		// User is scrolling with keyboard
		p.onScrollEvent()
		return event // Let the default handler process the scroll
	}
	return event
}

// handleInputFieldKeys handles key events for the input field
func (p *ProcessDetailPageView) handleInputFieldKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	}
	return event
}

// handleInputSubmit handles when input is submitted (Enter key)
func (p *ProcessDetailPageView) handleInputSubmit(key tcell.Key) {
	if key == tcell.KeyEnter {
		input := p.inputField.GetText()
		if input != "" && p.processID != "" {
			p.sendInput(input)
			p.inputField.SetText("")
		}
	}
}

// switchFocus switches focus between log view and input field
func (p *ProcessDetailPageView) switchFocus() {
	if p.FocusedItem == 0 {
		p.FocusedItem = 1
		p.tuiApp.app.SetFocus(p.inputField)
		p.logView.SetTitle(" Logs ")
		p.inputField.SetTitle(" Send Input (Press Enter to send) [FOCUSED] ")
	} else {
		p.FocusedItem = 0
		p.tuiApp.app.SetFocus(p.logView)
		p.logView.SetTitle(" Logs [FOCUSED] ")
		p.inputField.SetTitle(" Send Input (Press Enter to send) ")
	}
}

// toggleAutoScroll toggles auto-scroll for the log view
func (p *ProcessDetailPageView) toggleAutoScroll() {
	p.autoScroll = !p.autoScroll
	p.updateScrollStatus()
	
	// If enabling auto-scroll, scroll to end immediately
	if p.autoScroll {
		p.logView.ScrollToEnd()
		// Clear scroll lock when re-enabling auto-scroll
		p.isScrolling = false
	}
}

// sendInput sends input to the process stdin with IDIOMATIC ERROR HANDLING
func (p *ProcessDetailPageView) sendInput(input string) {
	if p.processID == "" {
		return
	}
	
	tracker, exists := GetProcessByID(p.processID)
	if !exists {
		return
	}
	
	tracker.Mutex.Lock()
	defer tracker.Mutex.Unlock()
	
	if tracker.Status != StatusRunning {
		return
	}
	
	if tracker.StdinWriter == nil {
		return
	}
	
	// Send input with newline
	finalInput := input + "\n"
	_, err := tracker.StdinWriter.Write([]byte(finalInput))
	if err != nil {
		// IDIOMATIC: Show error feedback in the log view
		p.appendToLogView(fmt.Sprintf("\n[ERROR] Failed to send input: %s\n", err.Error()))
		return
	}
	
	// Add the input to log view for visual feedback
	p.appendToLogView(fmt.Sprintf("\n[STDIN] %s\n", input))
}

// appendToLogView safely appends content to the log view
func (p *ProcessDetailPageView) appendToLogView(content string) {
	// IDIOMATIC: Use the Writer interface for thread-safe appends
	p.logView.Write([]byte(content))
}

// onScrollEvent marks that the user is actively scrolling
func (p *ProcessDetailPageView) onScrollEvent() {
	p.isScrolling = true
	p.lastScrollTime = time.Now()
	// Disable auto-scroll when user manually scrolls
	if p.autoScroll {
		p.autoScroll = false
		p.updateScrollStatus()
	}
}

// handleLogViewMouse handles mouse events for the log view
func (p *ProcessDetailPageView) handleLogViewMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	switch action {
	case tview.MouseScrollUp, tview.MouseScrollDown:
		// User is scrolling with mouse wheel
		p.onScrollEvent()
	case tview.MouseLeftDown, tview.MouseLeftClick:
		// User might be selecting text or clicking in the view
		p.onScrollEvent()
	}
	// Return the original action and event to let tview handle the scroll
	return action, event
}

// updateScrollStatus updates the log view title to show scroll status
func (p *ProcessDetailPageView) updateScrollStatus() {
	autoScrollStatus := "OFF"
	if p.autoScroll {
		autoScrollStatus = "ON"
	}
	
	title := fmt.Sprintf(" Logs [Auto-scroll: %s] ", autoScrollStatus)
	if p.FocusedItem == 0 {
		title += "[FOCUSED]"
	}
	p.logView.SetTitle(title)
}

// isScrollLocked returns true if updates should be paused due to recent scrolling
func (p *ProcessDetailPageView) isScrollLocked() bool {
	if !p.isScrolling {
		return false
	}
	
	// Check if scroll lock duration has passed
	if time.Since(p.lastScrollTime) > p.scrollLockDuration {
		p.isScrolling = false
		return false
	}
	
	return true
}

// SetProcess sets the current process to display
func (p *ProcessDetailPageView) SetProcess(processID string) {
	p.processID = processID
	p.lastLogContent = "" // Reset cache
	p.isScrolling = false // Reset scroll state
	p.autoScroll = true // Re-enable auto-scroll for new process
	p.updateScrollStatus()
	p.updateInfo()
	p.updateLogs()
}

// Refresh refreshes the page data
func (p *ProcessDetailPageView) Refresh() {
	if p.processID != "" {
		p.updateInfo()
		p.updateLogs()
	}
}

// Update updates the page with real-time data using INCREMENTAL UPDATES
func (p *ProcessDetailPageView) Update() {
	if p.processID != "" {
		p.updateInfo()
		p.updateLogsIncremental()
	}
}

// updateInfo updates the process information panel
func (p *ProcessDetailPageView) updateInfo() {
	if p.processID == "" {
		p.infoPanel.SetText("No process selected")
		return
	}
	
	tracker, exists := GetProcessByID(p.processID)
	if !exists {
		p.infoPanel.SetText("Process not found")
		return
	}
	
	tracker.Mutex.RLock()
	defer tracker.Mutex.RUnlock()
	
	// Calculate uptime
	uptime := time.Since(tracker.StartTime).Truncate(time.Second)
	
	// Format command
	command := tracker.Command
	if len(tracker.Args) > 0 {
		command += " " + strings.Join(tracker.Args, " ")
	}
	
	// Build info text
	info := fmt.Sprintf(`[yellow]ID:[white] %s
[yellow]Name:[white] %s
[yellow]PID:[white] %d
[yellow]Status:[white] %s
[yellow]Command:[white] %s
[yellow]Working Dir:[white] %s
[yellow]Session:[white] %s
[yellow]Start Time:[white] %s
[yellow]Uptime:[white] %s
[yellow]Buffer Size:[white] %s`,
		tracker.ID,
		getStringOrDash(tracker.Name),
		tracker.PID,
		string(tracker.Status),
		command,
		getStringOrDash(tracker.WorkingDir),
		getStringOrDash(tracker.SessionID),
		tracker.StartTime.Format("2006-01-02 15:04:05"),
		uptime.String(),
		formatBytes(tracker.BufferSize))
	
	if tracker.ExitCode != nil {
		info += fmt.Sprintf("\n[yellow]Exit Code:[white] %d", *tracker.ExitCode)
	}
	
	p.infoPanel.SetText(info)
}

// updateLogs updates the log viewer with process output (FULL UPDATE)
func (p *ProcessDetailPageView) updateLogs() {
	if p.processID == "" {
		return
	}
	
	tracker, exists := GetProcessByID(p.processID)
	if !exists {
		p.logView.SetText("Process not found")
		return
	}
	
	tracker.Mutex.RLock()
	defer tracker.Mutex.RUnlock()
	
	// Get combined output or separate streams
	var output string
	if tracker.CombineOutput {
		output = tracker.StdoutBuffer.GetContent()
	} else {
		stdout := tracker.StdoutBuffer.GetContent()
		stderr := tracker.StderrBuffer.GetContent()
		
		// Interleave stdout and stderr (simplified approach)
		if stdout != "" && stderr != "" {
			output = "[STDOUT]\n" + stdout + "\n[STDERR]\n" + stderr
		} else if stdout != "" {
			output = stdout
		} else if stderr != "" {
			output = stderr
		}
	}
	
	if output == "" {
		output = "No output available"
	}
	
	p.logView.SetText(output)
	p.lastLogContent = output
}

// updateLogsIncremental updates logs using IDIOMATIC INCREMENTAL PATTERN
func (p *ProcessDetailPageView) updateLogsIncremental() {
	if p.processID == "" {
		return
	}
	
	// Skip updates if user is actively scrolling
	if p.isScrollLocked() {
		return
	}
	
	tracker, exists := GetProcessByID(p.processID)
	if !exists {
		p.logView.SetText("Process not found")
		return
	}
	
	tracker.Mutex.RLock()
	defer tracker.Mutex.RUnlock()
	
	// Get current output
	var currentOutput string
	if tracker.CombineOutput {
		currentOutput = tracker.StdoutBuffer.GetContent()
	} else {
		stdout := tracker.StdoutBuffer.GetContent()
		stderr := tracker.StderrBuffer.GetContent()
		
		// Interleave stdout and stderr (simplified approach)
		if stdout != "" && stderr != "" {
			currentOutput = "[STDOUT]\n" + stdout + "\n[STDERR]\n" + stderr
		} else if stdout != "" {
			currentOutput = stdout
		} else if stderr != "" {
			currentOutput = stderr
		}
	}
	
	// IDIOMATIC INCREMENTAL UPDATE: Only update if content actually changed
	if currentOutput != p.lastLogContent {
		if currentOutput == "" {
			currentOutput = "No output available"
		}
		
		// Check if we can do an incremental append
		if p.lastLogContent != "" && strings.HasPrefix(currentOutput, p.lastLogContent) {
			// IDIOMATIC: Append only the new content
			newContent := currentOutput[len(p.lastLogContent):]
			if newContent != "" {
				p.appendToLogView(newContent)
				// Handle auto-scroll manually
				if p.autoScroll {
					p.logView.ScrollToEnd()
				}
			}
		} else {
			// Full update needed
			p.logView.SetText(currentOutput)
			// Handle auto-scroll after full update
			if p.autoScroll {
				p.logView.ScrollToEnd()
			}
			// Note: tview TextView doesn't support SetScrollOffset, so scroll position
			// may be lost on full updates. This is a limitation we accept to fix the freeze.
		}
		
		p.lastLogContent = currentOutput
	}
}

// getStringOrDash returns the string or "-" if empty
func getStringOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatBytes formats bytes in a human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetView returns the main view for this page
func (p *ProcessDetailPageView) GetView() tview.Primitive {
	return p.view
}