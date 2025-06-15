package main

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// AgentsQAPageView represents the agents Q&A tracking page - IDIOMATIC IMPLEMENTATION
type AgentsQAPageView struct {
	tuiApp          *TUIApp
	view            *tview.Flex
	qaTable         *tview.Table
	detailView      *tview.TextView
	statusBar       *tview.TextView
	selectedRow     int
	focusedItem     int // 0: table, 1: detail view
	lastQACount     int // Cache for incremental updates
	currentDetailID string
}

// NewAgentsQAPageView creates a new agents Q&A page view
func NewAgentsQAPageView(tuiApp *TUIApp) *AgentsQAPageView {
	p := &AgentsQAPageView{
		tuiApp:      tuiApp,
		qaTable:     tview.NewTable(),
		detailView:  tview.NewTextView(),
		statusBar:   tview.NewTextView(),
		selectedRow: 0,
		focusedItem: 0,
		lastQACount: 0,
	}

	p.setupTable()
	p.setupDetailView()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()

	return p
}

// setupTable configures the Q&A table
func (p *AgentsQAPageView) setupTable() {
	p.qaTable.SetBorder(true).SetTitle(" Agent Q&A History ").SetTitleAlign(tview.AlignLeft)
	p.qaTable.SetSelectable(true, false)
	p.qaTable.SetBorderPadding(0, 0, 1, 1)

	// Set table headers
	headers := []string{"Time", "From", "To", "Topic", "Status"}
	for col, header := range headers {
		textAlign := tview.AlignCenter
		if col == 3 { // Topic column
			textAlign = tview.AlignLeft
		}
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(textAlign).
			SetSelectable(false)
		if col == 3 {
			cell.SetExpansion(1) // Make topic column expand
		}
		p.qaTable.SetCell(0, col, cell)
	}

	p.qaTable.SetFixed(1, 0) // Fix the header row

	// Set up key handlers
	p.qaTable.SetInputCapture(p.handleTableKeys)
	p.qaTable.SetSelectionChangedFunc(p.handleSelectionChanged)
}

// setupDetailView configures the detail view
func (p *AgentsQAPageView) setupDetailView() {
	p.detailView.SetBorder(true).SetTitle(" Q&A Details ").SetTitleAlign(tview.AlignLeft)
	p.detailView.SetDynamicColors(true)
	p.detailView.SetWrap(true)
	p.detailView.SetWordWrap(true)
	p.detailView.SetInputCapture(p.handleDetailViewKeys)
	p.detailView.SetText("[gray]Select a Q&A entry to view details[white]")
}

// setupStatusBar configures the status bar
func (p *AgentsQAPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Tab[white]: Switch Focus | [yellow]Enter[white]: View Details | [yellow]Esc[white]: Back | [yellow]Q[white]: Quit")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *AgentsQAPageView) setupLayout() {
	// Main layout - horizontal split
	mainContent := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(p.qaTable, 0, 1, true).
		AddItem(p.detailView, 0, 1, false)

	// Vertical layout with status bar
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainContent, 0, 1, true).
		AddItem(p.statusBar, 4, 0, false)

	// Set up global key handlers
	p.view.SetInputCapture(p.handleGlobalKeys)
}

// handleGlobalKeys handles global key events for this page
func (p *AgentsQAPageView) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyEnter:
		if p.focusedItem == 0 {
			p.showSelectedDetails()
		}
		return nil
	}
	return event
}

// handleTableKeys handles key events for the table
func (p *AgentsQAPageView) handleTableKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	case tcell.KeyEnter:
		p.showSelectedDetails()
		return nil
	}
	return event
}

// handleDetailViewKeys handles key events for the detail view
func (p *AgentsQAPageView) handleDetailViewKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		p.switchFocus()
		return nil
	}
	return event
}

// handleSelectionChanged handles when table selection changes
func (p *AgentsQAPageView) handleSelectionChanged(row, col int) {
	p.selectedRow = row
	if row > 0 { // Skip header row
		p.showSelectedDetails()
	}
}

// switchFocus switches focus between table and detail view
func (p *AgentsQAPageView) switchFocus() {
	switch p.focusedItem {
	case 0: // Table -> Detail view
		p.focusedItem = 1
		p.tuiApp.app.SetFocus(p.detailView)
		p.qaTable.SetTitle(" Agent Q&A History ")
		p.detailView.SetTitle(" Q&A Details [FOCUSED] ")
	case 1: // Detail view -> Table
		p.focusedItem = 0
		p.tuiApp.app.SetFocus(p.qaTable)
		p.qaTable.SetTitle(" Agent Q&A History [FOCUSED] ")
		p.detailView.SetTitle(" Q&A Details ")
	}
}

// showSelectedDetails shows details for the selected Q&A entry
func (p *AgentsQAPageView) showSelectedDetails() {
	if p.selectedRow <= 0 || p.selectedRow >= p.qaTable.GetRowCount() {
		return
	}

	// Get Q&A ID from table (stored as reference in first cell)
	cell := p.qaTable.GetCell(p.selectedRow, 0)
	if cell == nil || cell.GetReference() == nil {
		return
	}

	qaID, ok := cell.GetReference().(string)
	if !ok {
		return
	}

	p.currentDetailID = qaID

	// Get Q&A details from registry
	qa := agentQARegistry.GetQA(qaID)
	if qa == nil {
		p.detailView.SetText("[red]Q&A entry not found[white]")
		return
	}

	// Format the detail view
	detail := fmt.Sprintf("[yellow]Question ID:[white] %s\n", qa.ID)
	detail += fmt.Sprintf("[yellow]Time:[white] %s\n", qa.Timestamp.Format("15:04:05"))
	detail += fmt.Sprintf("[yellow]From Agent:[white] %s\n", qa.From)
	detail += fmt.Sprintf("[yellow]To Specialist:[white] %s\n", qa.To)
	detail += fmt.Sprintf("[yellow]Status:[white] %s\n\n", p.getStatusColor(qa.Status))
	
	detail += "[yellow]Question:[white]\n"
	detail += qa.Question + "\n\n"

	if qa.Answer != "" {
		detail += "[yellow]Answer:[white]\n"
		detail += qa.Answer + "\n"
	} else if qa.Error != "" {
		detail += "[red]Error:[white]\n"
		detail += qa.Error + "\n"
	} else {
		detail += "[gray]Waiting for answer...[white]\n"
	}

	if qa.ProcessingTime > 0 {
		detail += fmt.Sprintf("\n[yellow]Processing Time:[white] %s", qa.ProcessingTime.Round(time.Millisecond))
	}

	p.detailView.SetText(detail)
}

// getStatusColor returns colored status text
func (p *AgentsQAPageView) getStatusColor(status QAStatus) string {
	switch status {
	case QAStatusPending:
		return "[yellow]Pending[white]"
	case QAStatusProcessing:
		return "[blue]Processing[white]"
	case QAStatusCompleted:
		return "[green]Completed[white]"
	case QAStatusFailed:
		return "[red]Failed[white]"
	case QAStatusTimeout:
		return "[red]Timeout[white]"
	default:
		return string(status)
	}
}

// Refresh refreshes the Q&A list
func (p *AgentsQAPageView) Refresh() {
	p.populateTable()
}

// Update updates the table with real-time data using IDIOMATIC INCREMENTAL UPDATES
func (p *AgentsQAPageView) Update() {
	p.populateTableIncremental()
	
	// Update current detail view if something is selected
	if p.currentDetailID != "" {
		p.showSelectedDetails()
	}
}

// populateTable populates the table with Q&A history (FULL REBUILD)
func (p *AgentsQAPageView) populateTable() {
	// Clear table except headers
	for row := p.qaTable.GetRowCount() - 1; row > 0; row-- {
		p.qaTable.RemoveRow(row)
	}

	// Get Q&A history
	qaList := agentQARegistry.GetAllQAs()

	// Sort by timestamp (newest first)
	// The registry should return them sorted already

	// Populate table with Q&A entries
	for i, qa := range qaList {
		row := i + 1

		// Format timestamp
		timeStr := qa.Timestamp.Format("15:04:05")

		// Truncate topic if too long
		topic := qa.Question
		if len(topic) > 40 {
			topic = topic[:37] + "..."
		}

		// Time cell with ID reference
		timeCell := tview.NewTableCell(timeStr).
			SetTextColor(tcell.ColorLightBlue).
			SetAlign(tview.AlignCenter).
			SetReference(qa.ID)
		
		// From cell
		fromCell := tview.NewTableCell(qa.From).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter)
		
		// To cell  
		toCell := tview.NewTableCell(qa.To).
			SetTextColor(tcell.ColorWhite).
			SetAlign(tview.AlignCenter)
		
		// Topic cell
		topicCell := tview.NewTableCell(topic).
			SetTextColor(tcell.ColorWhite).
			SetExpansion(1)
		
		// Status cell with color
		statusColor := tcell.ColorWhite
		switch qa.Status {
		case QAStatusPending:
			statusColor = tcell.ColorYellow
		case QAStatusProcessing:
			statusColor = tcell.ColorBlue
		case QAStatusCompleted:
			statusColor = tcell.ColorGreen
		case QAStatusFailed, QAStatusTimeout:
			statusColor = tcell.ColorRed
		}
		
		statusCell := tview.NewTableCell(string(qa.Status)).
			SetTextColor(statusColor).
			SetAlign(tview.AlignCenter)

		// Add cells
		p.qaTable.SetCell(row, 0, timeCell)
		p.qaTable.SetCell(row, 1, fromCell)
		p.qaTable.SetCell(row, 2, toCell)
		p.qaTable.SetCell(row, 3, topicCell)
		p.qaTable.SetCell(row, 4, statusCell)
	}

	// Update title with count
	title := fmt.Sprintf(" Agent Q&A History (%d) ", len(qaList))
	if p.focusedItem == 0 {
		title += "[FOCUSED]"
	}
	p.qaTable.SetTitle(title)

	// Restore selection if possible
	if p.selectedRow > 0 && p.selectedRow < p.qaTable.GetRowCount() {
		p.qaTable.Select(p.selectedRow, 0)
	} else if p.qaTable.GetRowCount() > 1 {
		p.qaTable.Select(1, 0) // Select first data row
	}

	p.lastQACount = len(qaList)
}

// populateTableIncremental uses IDIOMATIC INCREMENTAL UPDATE pattern
func (p *AgentsQAPageView) populateTableIncremental() {
	// Get current Q&A list
	qaList := agentQARegistry.GetAllQAs()

	// Check if we need to do a full rebuild
	currentCount := len(qaList)
	if currentCount != p.lastQACount {
		// Count changed - do full rebuild for simplicity
		// In a more advanced implementation, we could track individual changes
		p.populateTable()
		return
	}

	// Update existing entries (status might have changed)
	for i, qa := range qaList {
		row := i + 1
		if row >= p.qaTable.GetRowCount() {
			break
		}

		// Update status cell color
		statusColor := tcell.ColorWhite
		switch qa.Status {
		case QAStatusPending:
			statusColor = tcell.ColorYellow
		case QAStatusProcessing:
			statusColor = tcell.ColorBlue
		case QAStatusCompleted:
			statusColor = tcell.ColorGreen
		case QAStatusFailed, QAStatusTimeout:
			statusColor = tcell.ColorRed
		}

		statusCell := p.qaTable.GetCell(row, 4)
		if statusCell != nil {
			statusCell.SetText(string(qa.Status)).SetTextColor(statusColor)
		}
	}
}

// GetView returns the main view for this page
func (p *AgentsQAPageView) GetView() tview.Primitive {
	return p.view
}