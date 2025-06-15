package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// NotificationsPageView represents the notifications page - IDIOMATIC IMPLEMENTATION
type NotificationsPageView struct {
	tuiApp          *TUIApp
	view            *tview.Flex
	table           *tview.Table
	controlPanel    *tview.Flex
	soundToggle     *tview.Button
	clearButton     *tview.Button
	statusBar       *tview.TextView
	selectedRow     int
	focusedItem     int // 0: table, 1: sound toggle, 2: clear button
	lastHistorySize int // Cache for incremental updates
}

// NewNotificationsPageView creates a new notifications page view
func NewNotificationsPageView(tuiApp *TUIApp) *NotificationsPageView {
	p := &NotificationsPageView{
		tuiApp:          tuiApp,
		table:           tview.NewTable(),
		soundToggle:     tview.NewButton("Sound: ON"),
		clearButton:     tview.NewButton("Clear History"),
		statusBar:       tview.NewTextView(),
		selectedRow:     0,
		focusedItem:     0,
		lastHistorySize: 0,
	}
	
	p.setupTable()
	p.setupControls()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()
	
	return p
}

// setupTable configures the notifications table
func (p *NotificationsPageView) setupTable() {
	p.table.SetBorder(true).SetTitle(" Notification History ").SetTitleAlign(tview.AlignLeft)
	p.table.SetSelectable(true, false)
	p.table.SetBorderPadding(0, 0, 1, 1)
	
	// Set table headers
	headers := []string{"Time", "Message"}
	for col, header := range headers {
		if col == 0 {
			p.table.SetCell(0, col, tview.NewTableCell(header).
				SetTextColor(tcell.ColorYellow).
				SetAlign(tview.AlignCenter).
				SetSelectable(false))
		} else {
			p.table.SetCell(0, col, tview.NewTableCell(header).
				SetTextColor(tcell.ColorYellow).
				SetAlign(tview.AlignLeft).
				SetSelectable(false).
				SetExpansion(1)) // Make message column expand
		}
	}
	
	p.table.SetFixed(1, 0) // Fix the header row
	
	// Set up key handlers
	p.table.SetInputCapture(p.handleTableKeys)
	p.table.SetSelectionChangedFunc(p.handleSelectionChanged)
}

// setupControls configures the control buttons
func (p *NotificationsPageView) setupControls() {
	// Sound toggle button
	p.soundToggle.SetSelectedFunc(p.toggleSound)
	p.soundToggle.SetInputCapture(p.handleSoundToggleKeys)
	
	// Clear button
	p.clearButton.SetSelectedFunc(p.clearHistory)
	p.clearButton.SetInputCapture(p.handleClearButtonKeys)
	
	// Update button states
	p.updateSoundToggleText()
}

// setupStatusBar configures the status bar
func (p *NotificationsPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Tab[white]: Switch Focus | [yellow]Enter[white]: Activate | [yellow]Esc[white]: Back | [yellow]Q[white]: Quit\n[grey]Pages: [yellow]1[white]: Processes | [yellow]2[white]: Notifications | [yellow]3[white]: Logs | [yellow]4[white]: Agents Q&A[grey]")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *NotificationsPageView) setupLayout() {
	// Create control panel
	p.controlPanel = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(p.soundToggle, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false). // Spacer
		AddItem(p.clearButton, 0, 1, false)
	
	p.controlPanel.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	
	// Main layout
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.table, 0, 1, true).
		AddItem(p.controlPanel, 5, 0, false).
		AddItem(p.statusBar, 4, 0, false)
	
	// Set up global key handlers
	p.view.SetInputCapture(p.handleGlobalKeys)
}

// handleGlobalKeys handles global key events for this page
func (p *NotificationsPageView) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyEnter:
		p.activateCurrentItem()
		return nil
	}
	return event
}

// handleTableKeys handles key events for the table
func (p *NotificationsPageView) handleTableKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	}
	return event
}

// handleSoundToggleKeys handles key events for the sound toggle button
func (p *NotificationsPageView) handleSoundToggleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyEnter:
		p.toggleSound()
		return nil
	}
	return event
}

// handleClearButtonKeys handles key events for the clear button
func (p *NotificationsPageView) handleClearButtonKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyEnter:
		p.clearHistory()
		return nil
	}
	return event
}

// handleSelectionChanged handles when table selection changes
func (p *NotificationsPageView) handleSelectionChanged(row, col int) {
	p.selectedRow = row
}

// switchFocus switches focus between table and buttons
func (p *NotificationsPageView) switchFocus() {
	switch p.focusedItem {
	case 0: // Table -> Sound toggle
		p.focusedItem = 1
		p.tuiApp.app.SetFocus(p.soundToggle)
		p.table.SetTitle(" Notification History ")
		p.updateControlPanelTitle()
	case 1: // Sound toggle -> Clear button
		p.focusedItem = 2
		p.tuiApp.app.SetFocus(p.clearButton)
		p.updateControlPanelTitle()
	case 2: // Clear button -> Table
		p.focusedItem = 0
		p.tuiApp.app.SetFocus(p.table)
		p.table.SetTitle(" Notification History [FOCUSED] ")
		p.controlPanel.SetTitle(" Controls ")
	}
}

// updateControlPanelTitle updates the control panel title based on focus
func (p *NotificationsPageView) updateControlPanelTitle() {
	switch p.focusedItem {
	case 1:
		p.controlPanel.SetTitle(" Controls [Sound Toggle FOCUSED] ")
	case 2:
		p.controlPanel.SetTitle(" Controls [Clear FOCUSED] ")
	default:
		p.controlPanel.SetTitle(" Controls ")
	}
}

// activateCurrentItem activates the currently focused item
func (p *NotificationsPageView) activateCurrentItem() {
	switch p.focusedItem {
	case 1:
		p.toggleSound()
	case 2:
		p.clearHistory()
	}
}

// toggleSound toggles notification sound on/off
func (p *NotificationsPageView) toggleSound() {
	currentState := notificationManager.IsSoundEnabled()
	notificationManager.SetSoundEnabled(!currentState)
	p.updateSoundToggleText()
}

// updateSoundToggleText updates the sound toggle button text
func (p *NotificationsPageView) updateSoundToggleText() {
	if notificationManager.IsSoundEnabled() {
		p.soundToggle.SetLabel("Sound: ON")
		p.soundToggle.SetBackgroundColor(tcell.ColorGreen)
	} else {
		p.soundToggle.SetLabel("Sound: OFF")
		p.soundToggle.SetBackgroundColor(tcell.ColorRed)
	}
}

// clearHistory clears the notification history
func (p *NotificationsPageView) clearHistory() {
	notificationManager.ClearHistory()
	p.lastHistorySize = 0 // Reset cache
	p.Refresh()
}

// Refresh refreshes the notifications list
func (p *NotificationsPageView) Refresh() {
	p.populateTable()
	p.updateSoundToggleText()
}

// Update updates the table with real-time data using IDIOMATIC INCREMENTAL UPDATES
func (p *NotificationsPageView) Update() {
	p.populateTableIncremental()
	p.updateSoundToggleText()
}

// populateTable populates the table with notification history (FULL REBUILD)
func (p *NotificationsPageView) populateTable() {
	// Clear table except headers
	for row := p.table.GetRowCount() - 1; row > 0; row-- {
		p.table.RemoveRow(row)
	}
	
	// Get notification history
	history := notificationManager.GetHistory()
	
	// Populate table with history (newest first)
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		row := len(history) - i
		
		// Format timestamp
		timeStr := entry.Timestamp.Format("15:04:05")
		
		// Don't truncate message - let tview handle wrapping
		message := entry.Text
		
		// Add cells
		p.table.SetCell(row, 0, tview.NewTableCell(timeStr).
			SetTextColor(tcell.ColorLightBlue).
			SetAlign(tview.AlignCenter))
		p.table.SetCell(row, 1, tview.NewTableCell(message).
			SetTextColor(tcell.ColorWhite).
			SetExpansion(1))
	}
	
	// Update title with count
	title := fmt.Sprintf(" Notification History (%d) ", len(history))
	if p.focusedItem == 0 {
		title += "[FOCUSED]"
	}
	p.table.SetTitle(title)
	
	// Restore selection if possible
	if p.selectedRow > 0 && p.selectedRow < p.table.GetRowCount() {
		p.table.Select(p.selectedRow, 0)
	} else if p.table.GetRowCount() > 1 {
		p.table.Select(1, 0) // Select first data row
	}
	
	p.lastHistorySize = len(history)
}

// populateTableIncremental uses IDIOMATIC INCREMENTAL UPDATE pattern
func (p *NotificationsPageView) populateTableIncremental() {
	// Get current notification history
	history := notificationManager.GetHistory()
	
	// Check if we need to do a full rebuild (major changes)
	if len(history) < p.lastHistorySize || len(history) == 0 {
		// History was cleared or significantly changed - do full rebuild
		p.populateTable()
		return
	}
	
	// If only new items were added, append them incrementally
	if len(history) > p.lastHistorySize {
		// Add only the new entries
		newEntries := len(history) - p.lastHistorySize
		for i := 0; i < newEntries; i++ {
			entry := history[len(history)-1-i] // Get newest entries first
			row := p.table.GetRowCount() // Add at the end
			
			// Format timestamp
			timeStr := entry.Timestamp.Format("15:04:05")
			
			// Truncate message if too long
			message := entry.Text
			if len(message) > 80 {
				message = message[:77] + "..."
			}
			
			// IDIOMATIC: Insert row instead of rebuilding
			p.table.SetCell(row, 0, tview.NewTableCell(timeStr).SetTextColor(tcell.ColorLightBlue))
			p.table.SetCell(row, 1, tview.NewTableCell(message).SetTextColor(tcell.ColorWhite))
		}
		
		// Update the title with new count
		title := fmt.Sprintf(" Notification History (%d) ", len(history))
		if p.focusedItem == 0 {
			title += "[FOCUSED]"
		}
		p.table.SetTitle(title)
		
		p.lastHistorySize = len(history)
		
		// Auto-scroll to show newest notification if we're at the bottom
		if p.table.GetRowCount() > 1 {
			// Check if we were at the last row before adding
			currentRow, _ := p.table.GetSelection()
			if currentRow >= p.table.GetRowCount()-newEntries-1 {
				// We were near the bottom, scroll to show the newest
				p.table.Select(p.table.GetRowCount()-1, 0)
			}
		}
	} else if len(history) == p.lastHistorySize {
		// No changes - no update needed
		return
	}
}

// GetView returns the main view for this page
func (p *NotificationsPageView) GetView() tview.Primitive {
	return p.view
}