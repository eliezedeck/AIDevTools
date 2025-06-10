package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ProcessesPageView represents the processes list page - IDIOMATIC INCREMENTAL UPDATE IMPLEMENTATION
type ProcessesPageView struct {
	tuiApp          *TUIApp
	view            *tview.Flex
	table           *tview.Table
	statusBar       *tview.TextView
	reversedSort    bool
	lastProcessData map[string]*ProcessTracker // Cache for incremental updates
	lastSessionData map[string][]*ProcessTracker
	isInitialized   bool
}

// NewProcessesPageView creates a new processes page view using idiomatic tview patterns
func NewProcessesPageView(tuiApp *TUIApp) *ProcessesPageView {
	p := &ProcessesPageView{
		tuiApp:          tuiApp,
		table:           tview.NewTable(),
		statusBar:       tview.NewTextView(),
		reversedSort:    true, // Default to newest first
		lastProcessData: make(map[string]*ProcessTracker),
		lastSessionData: make(map[string][]*ProcessTracker),
		isInitialized:   false,
	}
	
	p.setupTable()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()
	
	return p
}

// setupTable configures the table using idiomatic tview patterns
func (p *ProcessesPageView) setupTable() {
	// Basic table setup - keep it simple
	p.table.SetBorder(true).SetTitle(" Processes ").SetTitleAlign(tview.AlignLeft)
	p.table.SetSelectable(true, false)
	p.table.SetBorderPadding(0, 0, 1, 1)
	
	// Fixed header row - idiomatic pattern
	p.table.SetFixed(1, 0)
	
	// Key handlers
	p.table.SetInputCapture(p.handleTableKeys)
	p.table.SetSelectedFunc(p.handleRowSelected)
	p.table.SetSelectionChangedFunc(p.handleSelectionChanged)
}

// setupStatusBar configures the status bar
func (p *ProcessesPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Enter[white]: View Details | [yellow]K[white]: Kill Process | [yellow]Del[white]: Remove Process | [yellow]R[white]: Sort | [yellow]Tab[white]: Switch Page | [yellow]Q[white]: Quit")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *ProcessesPageView) setupLayout() {
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.table, 0, 1, true).
		AddItem(p.statusBar, 3, 0, false)
}

// handleTableKeys handles key events for the table
func (p *ProcessesPageView) handleTableKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEnter:
		p.openSelectedProcess()
		return nil
	case tcell.KeyDelete:
		p.removeSelectedProcess()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'k', 'K':
			p.killSelectedProcess()
			return nil
		case 'r', 'R':
			p.toggleSort()
			return nil
		}
	}
	return event
}

// handleRowSelected handles when a row is selected (Enter key)
func (p *ProcessesPageView) handleRowSelected(row, col int) {
	p.openSelectedProcess()
}

// handleSelectionChanged handles when the selection changes
func (p *ProcessesPageView) handleSelectionChanged(row, col int) {
	// Track selection if needed
}

// openSelectedProcess opens the detail view for the selected process
func (p *ProcessesPageView) openSelectedProcess() {
	row, _ := p.table.GetSelection()
	if row <= 0 { // Skip header row
		return
	}
	
	// Get the process ID from the last column
	processIDCell := p.table.GetCell(row, 6) // ID column
	if processIDCell != nil && processIDCell.Text != "" {
		processID := processIDCell.Text
		p.tuiApp.ShowProcessDetail(processID)
	}
	// If it's a session header (no process ID), do nothing
}

// killSelectedProcess kills the currently selected process
func (p *ProcessesPageView) killSelectedProcess() {
	row, _ := p.table.GetSelection()
	if row <= 0 { // Skip header row
		return
	}
	
	// Get the process ID from the last column
	processIDCell := p.table.GetCell(row, 6) // ID column
	if processIDCell != nil && processIDCell.Text != "" {
		processID := processIDCell.Text
		// Get the process and kill it
		if tracker, exists := registry.getProcess(processID); exists {
			tracker.Mutex.Lock()
			defer tracker.Mutex.Unlock()
			
			if tracker.Status == StatusRunning && tracker.Process != nil && tracker.Process.Process != nil {
				// Close stdin first
				if tracker.StdinWriter != nil {
					tracker.StdinWriter.Close()
				}
				
				// Kill the process
				err := terminateProcessGroup(tracker.Process.Process.Pid)
				if err != nil {
					if tracker.Process.Process != nil {
						if killErr := tracker.Process.Process.Kill(); killErr != nil {
							// Process termination failed - likely already dead
						}
					}
				}
				tracker.Status = StatusKilled
				
				// Update display immediately with incremental update
				p.Update()
			}
		}
	}
}

// removeSelectedProcess removes the currently selected process from the registry
func (p *ProcessesPageView) removeSelectedProcess() {
	row, _ := p.table.GetSelection()
	if row <= 0 { // Skip header row
		return
	}
	
	// Get the process ID from the last column
	processIDCell := p.table.GetCell(row, 6) // ID column
	if processIDCell != nil && processIDCell.Text != "" {
		processID := processIDCell.Text
		// Remove the process from registry
		registry.removeProcess(processID)
		
		// Update display immediately with incremental update
		p.Update()
	}
}

// toggleSort toggles the sort order (newest first vs oldest first)
func (p *ProcessesPageView) toggleSort() {
	p.reversedSort = !p.reversedSort
	// Force full refresh when sort changes
	p.isInitialized = false
	p.Refresh()
}

// Refresh refreshes the processes list - FORCE FULL REBUILD
func (p *ProcessesPageView) Refresh() {
	p.isInitialized = false
	p.populateTableIncremental()
}

// Update updates the table with real-time data - IDIOMATIC INCREMENTAL UPDATES
func (p *ProcessesPageView) Update() {
	p.populateTableIncremental()
}

// populateTableIncremental uses IDIOMATIC INCREMENTAL UPDATE pattern to avoid visual jumps
func (p *ProcessesPageView) populateTableIncremental() {
	// Get current processes grouped by session
	sessionGroups := GetProcessesBySession(p.reversedSort)
	
	// If not initialized or major changes, do full rebuild
	if !p.isInitialized || p.majorChangesDetected(sessionGroups) {
		p.fullRebuild(sessionGroups)
		p.isInitialized = true
		p.lastSessionData = p.copySessionData(sessionGroups)
		p.updateProcessDataCache(sessionGroups)
		return
	}
	
	// IDIOMATIC INCREMENTAL UPDATES - only update what changed
	p.incrementalUpdate(sessionGroups)
	p.lastSessionData = p.copySessionData(sessionGroups)
	p.updateProcessDataCache(sessionGroups)
}

// majorChangesDetected checks if major structural changes occurred that require full rebuild
func (p *ProcessesPageView) majorChangesDetected(newSessionGroups map[string][]*ProcessTracker) bool {
	// Check if session structure changed
	if len(newSessionGroups) != len(p.lastSessionData) {
		return true
	}
	
	for sessionName := range newSessionGroups {
		if _, exists := p.lastSessionData[sessionName]; !exists {
			return true
		}
	}
	
	// Check if process count per session changed significantly
	for sessionName, processes := range newSessionGroups {
		if oldProcesses, exists := p.lastSessionData[sessionName]; exists {
			if len(processes) != len(oldProcesses) {
				return true
			}
		}
	}
	
	return false
}

// fullRebuild performs a full table rebuild - ONLY when necessary
func (p *ProcessesPageView) fullRebuild(sessionGroups map[string][]*ProcessTracker) {
	// Remember current selection
	currentRow, _ := p.table.GetSelection()
	var selectedProcessID string
	if currentRow > 0 && currentRow < p.table.GetRowCount() {
		if cell := p.table.GetCell(currentRow, 6); cell != nil && cell.Text != "" {
			selectedProcessID = cell.Text
		}
	}
	
	// ONLY clear when absolutely necessary - this is what causes the jump!
	p.table.Clear()
	
	// Build the table from scratch
	p.buildTableContent(sessionGroups, selectedProcessID)
}

// incrementalUpdate performs selective updates to existing table content
func (p *ProcessesPageView) incrementalUpdate(sessionGroups map[string][]*ProcessTracker) {
	// Track which rows need updates
	rowUpdates := make(map[int]bool)
	
	// Check each current table row for changes
	for row := 1; row < p.table.GetRowCount(); row++ {
		processIDCell := p.table.GetCell(row, 6)
		if processIDCell == nil || processIDCell.Text == "" {
			continue // Skip session headers
		}
		
		processID := processIDCell.Text
		
		// Find this process in the new data
		var currentProcess *ProcessTracker
		for _, processes := range sessionGroups {
			for _, process := range processes {
				if process.ID == processID {
					currentProcess = process
					break
				}
			}
			if currentProcess != nil {
				break
			}
		}
		
		// If process not found in new data, mark for update (will be removed)
		if currentProcess == nil {
			rowUpdates[row] = true
			continue
		}
		
		// Check if this process data changed
		if lastProcess, exists := p.lastProcessData[processID]; exists {
			if p.processDataChanged(lastProcess, currentProcess) {
				rowUpdates[row] = true
			}
		} else {
			rowUpdates[row] = true
		}
	}
	
	// Apply selective updates only to changed rows
	for row := range rowUpdates {
		if row < p.table.GetRowCount() {
			p.updateTableRow(row, sessionGroups)
		}
	}
	
	// Update the title and total count
	p.updateTableTitle(sessionGroups)
}

// updateTableRow updates a specific table row with new data
func (p *ProcessesPageView) updateTableRow(row int, sessionGroups map[string][]*ProcessTracker) {
	processIDCell := p.table.GetCell(row, 6)
	if processIDCell == nil || processIDCell.Text == "" {
		return // Skip session headers
	}
	
	processID := processIDCell.Text
	
	// Find the process in new data
	var currentProcess *ProcessTracker
	for _, processes := range sessionGroups {
		for _, process := range processes {
			if process.ID == processID {
				currentProcess = process
				break
			}
		}
		if currentProcess != nil {
			break
		}
	}
	
	if currentProcess == nil {
		// Process no longer exists - remove this row would require full rebuild
		// For now, mark it as removed in place
		p.table.GetCell(row, 1).SetText("REMOVED").SetTextColor(tcell.ColorRed)
		return
	}
	
	// Update each cell in this row
	currentProcess.Mutex.RLock()
	p.table.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("  %s", currentProcess.SessionID)).SetTextColor(tcell.ColorAqua))
	p.table.SetCell(row, 1, tview.NewTableCell(string(currentProcess.Status)).SetTextColor(getStatusColor(currentProcess.Status)))
	p.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", currentProcess.PID)).SetTextColor(tcell.ColorWhite))
	p.table.SetCell(row, 3, tview.NewTableCell(p.formatName(currentProcess)).SetTextColor(tcell.ColorGreen))
	p.table.SetCell(row, 4, tview.NewTableCell(p.formatCommand(currentProcess)).SetTextColor(tcell.ColorLightGray))
	p.table.SetCell(row, 5, tview.NewTableCell(currentProcess.StartTime.Format("15:04:05")).SetTextColor(tcell.ColorLightBlue))
	p.table.SetCell(row, 6, tview.NewTableCell(currentProcess.ID).SetTextColor(tcell.ColorDarkGray))
	currentProcess.Mutex.RUnlock()
}

// buildTableContent builds the complete table content
func (p *ProcessesPageView) buildTableContent(sessionGroups map[string][]*ProcessTracker, selectedProcessID string) {
	// Set header row
	headers := []string{"Session", "Status", "PID", "Name", "Command", "Start Time", "ID"}
	for col, header := range headers {
		p.table.SetCell(0, col, tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}
	
	// Get sorted session names
	sessionNames := make([]string, 0, len(sessionGroups))
	for sessionName := range sessionGroups {
		sessionNames = append(sessionNames, sessionName)
	}
	sort.Strings(sessionNames)
	
	row := 1 // Start after header
	totalProcesses := 0
	newSelectedRow := 1
	
	for _, sessionName := range sessionNames {
		processes := sessionGroups[sessionName]
		totalProcesses += len(processes)
		
		// Add session header row
		sessionText := fmt.Sprintf("📁 %s (%d processes)", sessionName, len(processes))
		sessionColor := tcell.ColorLime
		if p.getSessionStatus(processes) == "Inactive" {
			sessionColor = tcell.ColorGray
		}
		
		// Session header row - spans first column, others empty
		p.table.SetCell(row, 0, tview.NewTableCell(sessionText).SetTextColor(sessionColor))
		for col := 1; col < 7; col++ {
			p.table.SetCell(row, col, tview.NewTableCell("").SetSelectable(false))
		}
		row++
		
		// Add processes for this session
		for _, process := range processes {
			process.Mutex.RLock()
			
			// Track selection
			if process.ID == selectedProcessID {
				newSelectedRow = row
			}
			
			// Create process row
			p.table.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("  %s", process.SessionID)).SetTextColor(tcell.ColorAqua))
			p.table.SetCell(row, 1, tview.NewTableCell(string(process.Status)).SetTextColor(getStatusColor(process.Status)))
			p.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", process.PID)).SetTextColor(tcell.ColorWhite))
			p.table.SetCell(row, 3, tview.NewTableCell(p.formatName(process)).SetTextColor(tcell.ColorGreen))
			p.table.SetCell(row, 4, tview.NewTableCell(p.formatCommand(process)).SetTextColor(tcell.ColorLightGray))
			p.table.SetCell(row, 5, tview.NewTableCell(process.StartTime.Format("15:04:05")).SetTextColor(tcell.ColorLightBlue))
			p.table.SetCell(row, 6, tview.NewTableCell(process.ID).SetTextColor(tcell.ColorDarkGray))
			
			process.Mutex.RUnlock()
			row++
		}
	}
	
	// Update title
	p.updateTableTitle(sessionGroups)
	
	// Restore selection
	if p.table.GetRowCount() > 1 {
		if selectedProcessID != "" && newSelectedRow > 0 && newSelectedRow < p.table.GetRowCount() {
			p.table.Select(newSelectedRow, 0)
		} else {
			// Find first process row (not session header)
			for r := 1; r < p.table.GetRowCount(); r++ {
				if cell := p.table.GetCell(r, 6); cell != nil && cell.Text != "" {
					p.table.Select(r, 0)
					break
				}
			}
		}
	}
}

// updateTableTitle updates the table title with current information
func (p *ProcessesPageView) updateTableTitle(sessionGroups map[string][]*ProcessTracker) {
	totalProcesses := 0
	for _, processes := range sessionGroups {
		totalProcesses += len(processes)
	}
	
	sortOrder := "↓ Newest First"
	if !p.reversedSort {
		sortOrder = "↑ Oldest First"
	}
	title := fmt.Sprintf(" Processes (%d) - %s ", totalProcesses, sortOrder)
	p.table.SetTitle(title)
}

// processDataChanged checks if process data has changed between two instances
func (p *ProcessesPageView) processDataChanged(old, new *ProcessTracker) bool {
	old.Mutex.RLock()
	new.Mutex.RLock()
	defer old.Mutex.RUnlock()
	defer new.Mutex.RUnlock()
	
	return old.Status != new.Status ||
		old.PID != new.PID ||
		old.Name != new.Name ||
		old.SessionID != new.SessionID
}

// updateProcessDataCache updates the cached process data for change detection
func (p *ProcessesPageView) updateProcessDataCache(sessionGroups map[string][]*ProcessTracker) {
	newCache := make(map[string]*ProcessTracker)
	for _, processes := range sessionGroups {
		for _, process := range processes {
			// Create a copy for the cache
			process.Mutex.RLock()
			cachedProcess := &ProcessTracker{
				ID:        process.ID,
				Status:    process.Status,
				PID:       process.PID,
				Name:      process.Name,
				SessionID: process.SessionID,
			}
			process.Mutex.RUnlock()
			newCache[process.ID] = cachedProcess
		}
	}
	p.lastProcessData = newCache
}

// copySessionData creates a copy of session data for change detection
func (p *ProcessesPageView) copySessionData(sessionGroups map[string][]*ProcessTracker) map[string][]*ProcessTracker {
	copy := make(map[string][]*ProcessTracker)
	for sessionName, processes := range sessionGroups {
		sessionCopy := make([]*ProcessTracker, len(processes))
		for i, process := range processes {
			sessionCopy[i] = process
		}
		copy[sessionName] = sessionCopy
	}
	return copy
}

// getSessionStatus determines if a session is active based on its processes
func (p *ProcessesPageView) getSessionStatus(processes []*ProcessTracker) string {
	for _, process := range processes {
		process.Mutex.RLock()
		status := process.Status
		process.Mutex.RUnlock()
		
		if status == StatusRunning || status == StatusPending {
			return "Active"
		}
	}
	return "Inactive"
}

// formatName formats process name for display
func (p *ProcessesPageView) formatName(process *ProcessTracker) string {
	name := process.Name
	if name == "" {
		name = "-"
	}
	if len(name) > 15 {
		name = name[:12] + "..."
	}
	return name
}

// formatCommand formats process command for display
func (p *ProcessesPageView) formatCommand(process *ProcessTracker) string {
	command := process.Command
	if len(process.Args) > 0 {
		command += " " + strings.Join(process.Args, " ")
	}
	if len(command) > 40 {
		command = command[:37] + "..."
	}
	return command
}

// getStatusColor returns the appropriate color for a process status
func getStatusColor(status ProcessStatus) tcell.Color {
	switch status {
	case StatusRunning:
		return tcell.ColorGreen
	case StatusCompleted:
		return tcell.ColorBlue
	case StatusFailed:
		return tcell.ColorRed
	case StatusKilled:
		return tcell.ColorMaroon
	case StatusPending:
		return tcell.ColorYellow
	default:
		return tcell.ColorWhite
	}
}

// GetView returns the main view for this page
func (p *ProcessesPageView) GetView() tview.Primitive {
	return p.view
}