package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ProcessesPageView represents the processes list page
type ProcessesPageView struct {
	tuiApp        *TUIApp
	view          *tview.Flex
	table         *tview.Table
	statusBar     *tview.TextView
	reversedSort  bool
	selectedRow   int
}

// NewProcessesPageView creates a new processes page view
func NewProcessesPageView(tuiApp *TUIApp) *ProcessesPageView {
	p := &ProcessesPageView{
		tuiApp:       tuiApp,
		table:        tview.NewTable(),
		statusBar:    tview.NewTextView(),
		reversedSort: true, // Default to newest first
		selectedRow:  0,
	}
	
	p.setupTable()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()
	
	return p
}

// setupTable configures the table
func (p *ProcessesPageView) setupTable() {
	p.table.SetBorder(true).SetTitle(" Processes ").SetTitleAlign(tview.AlignLeft)
	p.table.SetSelectable(true, false)
	p.table.SetBorderPadding(0, 0, 1, 1)
	
	// Set table headers
	headers := []string{"Session", "Status", "PID", "Name", "Command", "Start Time", "ID"}
	for col, header := range headers {
		p.table.SetCell(0, col, tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}
	
	// Set column widths and alignment
	p.table.SetFixed(1, 0) // Fix the header row
	
	// Set up key handlers
	p.table.SetInputCapture(p.handleTableKeys)
	
	// Set up selection handler
	p.table.SetSelectedFunc(p.handleRowSelected)
	p.table.SetSelectionChangedFunc(p.handleSelectionChanged)
}

// setupStatusBar configures the status bar
func (p *ProcessesPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Enter[white]: View Details | [yellow]R[white]: Toggle Sort | [yellow]Tab[white]: Switch Page | [yellow]Q[white]: Quit")
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
	case tcell.KeyRune:
		switch event.Rune() {
		case 'r', 'R':
			p.toggleSort()
			return nil
		}
	}
	return event
}

// handleRowSelected handles when a row is selected (Enter key or double-click)
func (p *ProcessesPageView) handleRowSelected(row, col int) {
	p.openSelectedProcess()
}

// handleSelectionChanged handles when the selection changes
func (p *ProcessesPageView) handleSelectionChanged(row, col int) {
	p.selectedRow = row
}

// openSelectedProcess opens the detail view for the selected process
func (p *ProcessesPageView) openSelectedProcess() {
	row, _ := p.table.GetSelection()
	if row <= 0 { // Skip header row
		return
	}
	
	// Get the process ID from the last column
	processIDCell := p.table.GetCell(row, 6) // ID column
	if processIDCell != nil {
		processID := processIDCell.Text
		if processID != "" {
			p.tuiApp.ShowProcessDetail(processID)
		}
	}
}

// toggleSort toggles the sort order (newest first vs oldest first)
func (p *ProcessesPageView) toggleSort() {
	p.reversedSort = !p.reversedSort
	p.Refresh()
}

// Refresh refreshes the processes list
func (p *ProcessesPageView) Refresh() {
	p.populateTable()
}

// Update updates the table with real-time data
func (p *ProcessesPageView) Update() {
	p.populateTable()
}

// populateTable populates the table with current process data
func (p *ProcessesPageView) populateTable() {
	// Clear table except headers
	for row := p.table.GetRowCount() - 1; row > 0; row-- {
		p.table.RemoveRow(row)
	}
	
	// Get processes grouped by session
	sessionGroups := GetProcessesBySession(p.reversedSort)
	
	// Get sorted session names
	sessionNames := make([]string, 0, len(sessionGroups))
	for sessionName := range sessionGroups {
		sessionNames = append(sessionNames, sessionName)
	}
	sort.Strings(sessionNames)
	
	row := 1
	totalProcesses := 0
	
	for _, sessionName := range sessionNames {
		processes := sessionGroups[sessionName]
		totalProcesses += len(processes)
		
		// Add processes for this session
		for _, process := range processes {
			process.Mutex.RLock()
			
			// Status with color
			status := string(process.Status)
			statusColor := getStatusColor(process.Status)
			
			// Format start time
			startTime := process.StartTime.Format("15:04:05")
			
			// Truncate command for display
			command := process.Command
			if len(process.Args) > 0 {
				command += " " + strings.Join(process.Args, " ")
			}
			if len(command) > 40 {
				command = command[:37] + "..."
			}
			
			// Truncate name for display
			name := process.Name
			if name == "" {
				name = "-"
			}
			if len(name) > 15 {
				name = name[:12] + "..."
			}
			
			// Truncate process ID for display
			id := process.ID
			if len(id) > 8 {
				id = id[:8] + "..."
			}
			
			// Add cells
			p.table.SetCell(row, 0, tview.NewTableCell(sessionName).SetTextColor(tcell.ColorAqua))
			p.table.SetCell(row, 1, tview.NewTableCell(status).SetTextColor(statusColor))
			p.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", process.PID)).SetTextColor(tcell.ColorWhite))
			p.table.SetCell(row, 3, tview.NewTableCell(name).SetTextColor(tcell.ColorGreen))
			p.table.SetCell(row, 4, tview.NewTableCell(command).SetTextColor(tcell.ColorLightGray))
			p.table.SetCell(row, 5, tview.NewTableCell(startTime).SetTextColor(tcell.ColorLightBlue))
			p.table.SetCell(row, 6, tview.NewTableCell(process.ID).SetTextColor(tcell.ColorDarkGray))
			
			process.Mutex.RUnlock()
			row++
		}
	}
	
	// Update title with count and sort order
	sortOrder := "↓ Newest First"
	if !p.reversedSort {
		sortOrder = "↑ Oldest First"
	}
	title := fmt.Sprintf(" Processes (%d) - %s ", totalProcesses, sortOrder)
	p.table.SetTitle(title)
	
	// Restore selection if possible
	if p.selectedRow > 0 && p.selectedRow < p.table.GetRowCount() {
		p.table.Select(p.selectedRow, 0)
	} else if p.table.GetRowCount() > 1 {
		p.table.Select(1, 0) // Select first data row
	}
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