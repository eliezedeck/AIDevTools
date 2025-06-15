package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// LogsPageView represents the logs view in the TUI - IDIOMATIC IMPLEMENTATION
type LogsPageView struct {
	tuiApp        *TUIApp
	view          *tview.Flex
	table         *tview.Table
	controlPanel  *tview.Flex
	filterButton  *tview.Button
	clearButton   *tview.Button
	statusBar     *tview.TextView
	selectedRow   int
	focusedItem   int // 0: table, 1: filter button, 2: clear button
	showAllLevels bool
	filterLevel   LogLevel
}

// NewLogsPageView creates a new logs page view
func NewLogsPageView(tuiApp *TUIApp) *LogsPageView {
	p := &LogsPageView{
		tuiApp:        tuiApp,
		table:         tview.NewTable(),
		filterButton:  tview.NewButton("Filter: All"),
		clearButton:   tview.NewButton("Clear Logs"),
		statusBar:     tview.NewTextView(),
		selectedRow:   0,
		focusedItem:   0,
		showAllLevels: true,
	}

	p.setupTable()
	p.setupControls()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()

	return p
}

// setupTable configures the logs table
func (p *LogsPageView) setupTable() {
	p.table.SetBorder(true).SetTitle(" System Logs ").SetTitleAlign(tview.AlignLeft)
	p.table.SetSelectable(true, false)
	p.table.SetBorderPadding(0, 0, 1, 1)

	// Set table headers
	headers := []string{"Time", "Level", "Source", "Message"}
	for i, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		p.table.SetCell(0, i, cell)
	}

	// Handle table selection changes
	p.table.SetSelectionChangedFunc(func(row, column int) {
		p.selectedRow = row
		p.updateStatusBar()
	})

	// Handle key events
	p.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if p.focusedItem != 0 {
			return event
		}

		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'f', 'F':
				p.toggleFilter()
				return nil
			case 'c', 'C':
				p.clearLogs()
				return nil
			}
		}
		return event
	})

	p.table.SetBackgroundColor(tcell.ColorBlack)
}

// setupControls configures the control buttons
func (p *LogsPageView) setupControls() {
	// Filter button setup
	p.filterButton.SetSelectedFunc(func() {
		p.toggleFilter()
	})

	// Clear button setup
	p.clearButton.SetSelectedFunc(func() {
		p.clearLogs()
	})

	// Style the buttons
	p.filterButton.SetBackgroundColor(tcell.ColorDarkBlue)
	p.clearButton.SetBackgroundColor(tcell.ColorDarkRed)
}

// setupStatusBar configures the status bar
func (p *LogsPageView) setupStatusBar() {
	p.statusBar.SetDynamicColors(true)
	p.statusBar.SetText("[grey]Press Tab to switch panels | f: Filter | c: Clear | ↑↓: Navigate[white]")
	p.statusBar.SetBorder(true).SetBorderPadding(0, 0, 1, 1)
	p.statusBar.SetBackgroundColor(tcell.ColorBlack)
}

// setupLayout creates the page layout
func (p *LogsPageView) setupLayout() {
	// Create control panel with buttons
	p.controlPanel = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(p.filterButton, 0, 1, false).
		AddItem(tview.NewBox(), 1, 0, false). // Spacer
		AddItem(p.clearButton, 0, 1, false)
	p.controlPanel.SetBackgroundColor(tcell.ColorBlack)

	// Create main layout
	p.view = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(p.table, 0, 1, true).
		AddItem(p.controlPanel, 3, 0, false).
		AddItem(p.statusBar, 3, 0, false)
	p.view.SetBackgroundColor(tcell.ColorBlack)

	// Handle input capture for navigation between components
	p.view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			p.focusNext()
			return nil
		case tcell.KeyBacktab:
			p.focusPrev()
			return nil
		}
		return event
	})
}

// GetView returns the view component
func (p *LogsPageView) GetView() tview.Primitive {
	return p.view
}

// Refresh updates the logs display
func (p *LogsPageView) Refresh() {
	// Clear existing rows (except header)
	p.table.Clear()

	// Re-add headers
	headers := []string{"Time", "Level", "Source", "Message"}
	for i, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		p.table.SetCell(0, i, cell)
	}

	// Get logs based on filter
	var logs []LogEntry
	if p.showAllLevels {
		logs = GetLogEntries()
	} else {
		logs = logger.GetEntriesByLevel(p.filterLevel)
	}

	// Update table title with count
	title := fmt.Sprintf(" System Logs (%d entries) ", len(logs))
	p.table.SetTitle(title)

	// Add log entries
	for i, log := range logs {
		row := i + 1 // Account for header row

		// Time column
		timeCell := tview.NewTableCell(log.Timestamp.Format("15:04:05.000")).
			SetTextColor(tcell.ColorWhite)
		p.table.SetCell(row, 0, timeCell)

		// Level column with color
		levelCell := tview.NewTableCell(log.Level.String())
		switch log.Level {
		case LogLevelInfo:
			levelCell.SetTextColor(tcell.ColorGreen)
		case LogLevelWarn:
			levelCell.SetTextColor(tcell.ColorYellow)
		case LogLevelError:
			levelCell.SetTextColor(tcell.ColorRed)
		}
		p.table.SetCell(row, 1, levelCell)

		// Source column
		sourceCell := tview.NewTableCell(log.Source).
			SetTextColor(tcell.ColorWhite)
		p.table.SetCell(row, 2, sourceCell)

		// Message column
		message := log.Message
		if log.Details != "" {
			message = fmt.Sprintf("%s [%s]", message, log.Details)
		}
		messageCell := tview.NewTableCell(message).
			SetTextColor(tcell.ColorWhite).
			SetExpansion(1) // Allow message to expand
		p.table.SetCell(row, 3, messageCell)
	}

	// Update filter button text
	if p.showAllLevels {
		p.filterButton.SetLabel("Filter: All")
	} else {
		p.filterButton.SetLabel(fmt.Sprintf("Filter: %s", p.filterLevel.String()))
	}

	p.updateStatusBar()
}

// toggleFilter cycles through filter options
func (p *LogsPageView) toggleFilter() {
	if p.showAllLevels {
		p.showAllLevels = false
		p.filterLevel = LogLevelError
	} else if p.filterLevel == LogLevelError {
		p.filterLevel = LogLevelWarn
	} else if p.filterLevel == LogLevelWarn {
		p.filterLevel = LogLevelInfo
	} else {
		p.showAllLevels = true
	}
	p.Refresh()
}

// clearLogs clears all log entries
func (p *LogsPageView) clearLogs() {
	ClearLogs()
	p.Refresh()
}

// updateStatusBar updates the status bar text
func (p *LogsPageView) updateStatusBar() {
	logs := GetLogEntries()
	if p.selectedRow > 0 && p.selectedRow <= len(logs) {
		log := logs[p.selectedRow-1]
		if log.Details != "" {
			p.statusBar.SetText(fmt.Sprintf("[yellow]Details:[white] %s", log.Details))
			return
		}
	}
	p.statusBar.SetText("[grey]Press Tab to switch panels | f: Filter | c: Clear | ↑↓: Navigate[white]")
}

// focusNext moves focus to the next control
func (p *LogsPageView) focusNext() {
	p.focusedItem = (p.focusedItem + 1) % 3
	p.updateFocus()
}

// focusPrev moves focus to the previous control
func (p *LogsPageView) focusPrev() {
	p.focusedItem = (p.focusedItem + 2) % 3
	p.updateFocus()
}

// updateFocus updates which control has focus
func (p *LogsPageView) updateFocus() {
	switch p.focusedItem {
	case 0:
		p.tuiApp.app.SetFocus(p.table)
	case 1:
		p.tuiApp.app.SetFocus(p.filterButton)
	case 2:
		p.tuiApp.app.SetFocus(p.clearButton)
	}
}
