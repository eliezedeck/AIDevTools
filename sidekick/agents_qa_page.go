package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// AgentsQAPageView represents the agents Q&A tracking page - IDIOMATIC IMPLEMENTATION
type AgentsQAPageView struct {
	tuiApp               *TUIApp
	view                 *tview.Flex
	qaTable              *tview.Table
	detailView           *tview.TextView
	statusBar            *tview.TextView
	selectedRow          int
	focusedItem          int                          // 0: table, 1: detail view
	lastQACount          int                          // Cache for incremental updates
	lastSpecialistData   map[string][]*QuestionAnswer // Cache for incremental updates
	lastSpecialistStatus map[string]string            // Cache for specialist status tracking
	currentDetailID      string
	isInitialized        bool
}

// NewAgentsQAPageView creates a new agents Q&A page view
func NewAgentsQAPageView(tuiApp *TUIApp) *AgentsQAPageView {
	p := &AgentsQAPageView{
		tuiApp:               tuiApp,
		qaTable:              tview.NewTable(),
		detailView:           tview.NewTextView(),
		statusBar:            tview.NewTextView(),
		selectedRow:          0,
		focusedItem:          0,
		lastQACount:          0,
		lastSpecialistData:   make(map[string][]*QuestionAnswer),
		lastSpecialistStatus: make(map[string]string),
		isInitialized:        false,
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
	p.qaTable.SetBorder(true).SetTitle(" Q&A History ").SetTitleAlign(tview.AlignLeft)
	p.qaTable.SetSelectable(true, false)
	p.qaTable.SetBorderPadding(0, 0, 1, 1)

	// Fixed header row - idiomatic pattern
	p.qaTable.SetFixed(1, 0)

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
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Enter[white]: View Details | [yellow]Tab[white]: Switch Focus | [yellow]Q[white]: Quit\n[grey]Pages: [yellow]1[white]: Processes | [yellow]2[white]: Notifications | [yellow]3[white]: Logs | [yellow]4[white]: Agents Q&A[grey]")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *AgentsQAPageView) setupLayout() {
	// Main layout - 2 columns: table (60%) and detail view (40%)
	mainContent := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(p.qaTable, 0, 3, true).
		AddItem(p.detailView, 0, 2, false)

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
		p.qaTable.SetTitle(" Q&A History ")
		p.detailView.SetTitle(" Q&A Details [FOCUSED] ")
	case 1: // Detail view -> Table
		p.focusedItem = 0
		p.tuiApp.app.SetFocus(p.qaTable)
		p.qaTable.SetTitle(" Q&A History [FOCUSED] ")
		p.detailView.SetTitle(" Q&A Details ")
	}
}

// showSelectedDetails shows details for the selected Q&A entry or directory
func (p *AgentsQAPageView) showSelectedDetails() {
	if p.selectedRow <= 0 || p.selectedRow >= p.qaTable.GetRowCount() {
		return
	}

	// Get reference from table (stored as reference in first cell)
	cell := p.qaTable.GetCell(p.selectedRow, 0)
	if cell == nil || cell.GetReference() == nil {
		return
	}

	// Check if it's a directory key or Q&A ID
	if dirKey, ok := cell.GetReference().(string); ok {
		// Check if this is a directory (contains hyphen between rootdir and specialty)
		if strings.Contains(dirKey, "-") && agentQARegistry.GetDirectory(dirKey) != nil {
			// Show directory details
			p.showDirectoryDetails(dirKey)
			return
		}

		// Otherwise it's a Q&A ID
		p.currentDetailID = dirKey

		// Get Q&A details from registry
		qa := agentQARegistry.GetQA(dirKey)
		if qa == nil {
			p.detailView.SetText("[red]Q&A entry not found[white]")
			return
		}

		// Format the detail view - processing time at the top
		detail := ""
		if qa.ProcessingTime > 0 {
			detail += fmt.Sprintf("[yellow]Processing Time:[white] %s\n\n", qa.ProcessingTime.Round(time.Millisecond))
		}

		detail += fmt.Sprintf("[yellow]Question ID:[white] %s\n", qa.ID)
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

		p.detailView.SetText(detail)
	}
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

// Refresh refreshes the Q&A table - FORCE FULL REBUILD
func (p *AgentsQAPageView) Refresh() {
	p.isInitialized = false
	p.populateTableIncremental()
}

// Update updates the table with real-time data - IDIOMATIC INCREMENTAL UPDATES
func (p *AgentsQAPageView) Update() {
	p.populateTableIncremental()

	// Update current detail view if something is selected
	if p.currentDetailID != "" {
		p.showSelectedDetails()
	}
}

// populateTableIncremental uses IDIOMATIC INCREMENTAL UPDATE pattern to avoid visual jumps
func (p *AgentsQAPageView) populateTableIncremental() {
	// Get current Q&As grouped by specialist
	specialistGroups := p.getQAsBySpecialist()

	// If not initialized or major changes, do full rebuild
	if !p.isInitialized || p.majorChangesDetected(specialistGroups) {
		p.fullRebuild(specialistGroups)
		p.isInitialized = true
		p.lastSpecialistData = p.copySpecialistData(specialistGroups)
		p.lastSpecialistStatus = p.copySpecialistStatus()
		return
	}

	// IDIOMATIC INCREMENTAL UPDATES - only update what changed
	p.incrementalUpdate(specialistGroups)
	p.lastSpecialistData = p.copySpecialistData(specialistGroups)
	p.lastSpecialistStatus = p.copySpecialistStatus()
}

// getQAsBySpecialist returns Q&As grouped by directory
func (p *AgentsQAPageView) getQAsBySpecialist() map[string][]*QuestionAnswer {
	allDirectories := agentQARegistry.ListDirectories()

	// Group by directory key
	directoryGroups := make(map[string][]*QuestionAnswer)
	for _, dir := range allDirectories {
		// Get Q&As for this directory
		qas := agentQARegistry.GetQAsByDirectory(dir.Key)
		directoryGroups[dir.Key] = qas
	}

	return directoryGroups
}

// majorChangesDetected checks if major changes occurred that require full rebuild
func (p *AgentsQAPageView) majorChangesDetected(specialistGroups map[string][]*QuestionAnswer) bool {
	// Check if number of specialists changed
	if len(specialistGroups) != len(p.lastSpecialistData) {
		return true
	}

	// Check if any specialist has different number of Q&As
	for specialist, qas := range specialistGroups {
		if lastQAs, exists := p.lastSpecialistData[specialist]; !exists || len(qas) != len(lastQAs) {
			return true
		}
	}

	// Check if any new directories were created
	currentDirectories := agentQARegistry.ListDirectories()
	for _, dir := range currentDirectories {
		if _, exists := p.lastSpecialistStatus[dir.Key]; !exists {
			// New directory created - trigger full rebuild
			return true
		}
	}

	return false
}

// copySpecialistData creates a copy of specialist data for caching
func (p *AgentsQAPageView) copySpecialistData(specialistGroups map[string][]*QuestionAnswer) map[string][]*QuestionAnswer {
	copy := make(map[string][]*QuestionAnswer)
	for specialist, qas := range specialistGroups {
		copy[specialist] = make([]*QuestionAnswer, len(qas))
		for i, qa := range qas {
			copy[specialist][i] = qa
		}
	}
	return copy
}

// copySpecialistStatus creates a copy of directory keys for caching
func (p *AgentsQAPageView) copySpecialistStatus() map[string]string {
	copy := make(map[string]string)
	directories := agentQARegistry.ListDirectories()
	for _, dir := range directories {
		copy[dir.Key] = "exists"
	}
	return copy
}

// fullRebuild performs a full table rebuild - ONLY when necessary
func (p *AgentsQAPageView) fullRebuild(specialistGroups map[string][]*QuestionAnswer) {
	// Remember current selection
	currentRow, _ := p.qaTable.GetSelection()
	var selectedQAID string
	if currentRow > 0 && currentRow < p.qaTable.GetRowCount() {
		if cell := p.qaTable.GetCell(currentRow, 0); cell != nil && cell.GetReference() != nil {
			if qaID, ok := cell.GetReference().(string); ok {
				selectedQAID = qaID
			}
		}
	}

	// ONLY clear when absolutely necessary - this is what causes the jump!
	p.qaTable.Clear()

	// Build the table from scratch
	p.buildTableContent(specialistGroups, selectedQAID)
}

// incrementalUpdate performs selective updates to existing table content
func (p *AgentsQAPageView) incrementalUpdate(specialistGroups map[string][]*QuestionAnswer) {
	// For now, do a simple status update check
	// In a more sophisticated implementation, we could track individual Q&A changes

	// Update the title and total count
	p.updateTableTitle(specialistGroups)

	// Update specialist headers and Q&A status colors
	for row := 1; row < p.qaTable.GetRowCount(); row++ {
		cell := p.qaTable.GetCell(row, 0)
		if cell == nil {
			continue
		}

		if cell.GetReference() == nil {
			// This is a specialist header row (no reference means it's not a Q&A)
			p.updateSpecialistHeaderRow(row, cell, specialistGroups)
		} else {
			// This is a Q&A row (has reference)
			if qaID, ok := cell.GetReference().(string); ok {
				qa := agentQARegistry.GetQA(qaID)
				if qa != nil {
					// Update status cell color (column 1, not 3)
					statusCell := p.qaTable.GetCell(row, 1)
					if statusCell != nil {
						statusColor := p.getStatusColor2(qa.Status)
						statusCell.SetText(string(qa.Status)).SetTextColor(statusColor)
					}
				}
			}
		}
	}
}

// updateSpecialistHeaderRow updates a directory header row with current status
func (p *AgentsQAPageView) updateSpecialistHeaderRow(row int, cell *tview.TableCell, specialistGroups map[string][]*QuestionAnswer) {
	// Check if this is a directory header (has reference)
	if cell.GetReference() == nil {
		return
	}

	dirKey, ok := cell.GetReference().(string)
	if !ok {
		return
	}

	// Get directory info
	dir := agentQARegistry.GetDirectory(dirKey)
	if dir == nil {
		return
	}

	// Get Q&As for this directory
	qas := specialistGroups[dirKey]
	if qas == nil {
		qas = []*QuestionAnswer{}
	}

	// Count pending questions
	pendingCount := 0
	for _, qa := range qas {
		if qa.Status == QAStatusPending {
			pendingCount++
		}
	}

	// Rebuild the directory text with current status
	directoryText := fmt.Sprintf("📁 %s (%s) - %d pending",
		dir.RootDir, dir.Specialty, pendingCount)

	// Set color based on pending questions
	directoryColor := tcell.ColorLime // Default
	if pendingCount > 0 {
		directoryColor = tcell.ColorYellow
	}

	// Update the cell with new text and color
	cell.SetText(directoryText).SetTextColor(directoryColor)
}

// buildTableContent builds the complete table content
func (p *AgentsQAPageView) buildTableContent(specialistGroups map[string][]*QuestionAnswer, selectedQAID string) {
	// Set header row
	headers := []string{"Directory / From", "Status", "Question", "Time"}
	for col, header := range headers {
		p.qaTable.SetCell(0, col, tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	// Get sorted directory keys
	directoryKeys := make([]string, 0, len(specialistGroups))
	for dirKey := range specialistGroups {
		directoryKeys = append(directoryKeys, dirKey)
	}
	sort.Strings(directoryKeys)

	row := 1 // Start after header
	totalQAs := 0
	newSelectedRow := 1

	for _, dirKey := range directoryKeys {
		qas := specialistGroups[dirKey]
		totalQAs += len(qas)

		// Get directory info for display
		dir := agentQARegistry.GetDirectory(dirKey)
		if dir == nil {
			continue
		}

		// Count pending questions
		pendingCount := 0
		for _, qa := range qas {
			if qa.Status == QAStatusPending {
				pendingCount++
			}
		}

		// Add directory header row
		directoryText := fmt.Sprintf("📁 %s (%s) - %d pending",
			dir.RootDir, dir.Specialty, pendingCount)

		// Set color based on pending questions
		directoryColor := tcell.ColorLime // Default
		if pendingCount > 0 {
			directoryColor = tcell.ColorYellow
		}

		// Directory header row - spans first column, others empty
		p.qaTable.SetCell(row, 0, tview.NewTableCell(directoryText).SetTextColor(directoryColor).SetReference(dirKey))
		for col := 1; col < 4; col++ {
			p.qaTable.SetCell(row, col, tview.NewTableCell("").SetSelectable(false))
		}
		row++

		// Add Q&A rows for this directory
		for _, qa := range qas {
			// Check if this should be the selected row
			if qa.ID == selectedQAID {
				newSelectedRow = row
			}

			// Create Q&A row - indented under directory
			p.qaTable.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("  %s", qa.From)).SetTextColor(tcell.ColorAqua).SetReference(qa.ID))
			p.qaTable.SetCell(row, 1, tview.NewTableCell(string(qa.Status)).SetTextColor(p.getStatusColor2(qa.Status)))

			// Truncate question if too long
			question := qa.Question
			if len(question) > 50 {
				question = question[:47] + "..."
			}
			p.qaTable.SetCell(row, 2, tview.NewTableCell(question).SetTextColor(tcell.ColorWhite))
			p.qaTable.SetCell(row, 3, tview.NewTableCell(qa.Timestamp.Format("15:04:05")).SetTextColor(tcell.ColorLightBlue))

			row++
		}
	}

	// Update title and restore selection
	p.updateTableTitle(specialistGroups)

	// Restore selection
	if newSelectedRow > 0 && newSelectedRow < p.qaTable.GetRowCount() {
		p.qaTable.Select(newSelectedRow, 0)
	} else if p.qaTable.GetRowCount() > 1 {
		p.qaTable.Select(1, 0) // Select first data row
	}
}

// updateTableTitle updates the table title with current information
func (p *AgentsQAPageView) updateTableTitle(specialistGroups map[string][]*QuestionAnswer) {
	totalQAs := 0
	for _, qas := range specialistGroups {
		totalQAs += len(qas)
	}

	title := fmt.Sprintf(" Q&A History (%d) ", totalQAs)
	if p.focusedItem == 0 {
		title += "[FOCUSED]"
	}
	p.qaTable.SetTitle(title)
}

// getStatusColor2 returns the color for a status
func (p *AgentsQAPageView) getStatusColor2(status QAStatus) tcell.Color {
	switch status {
	case QAStatusPending:
		return tcell.ColorYellow
	case QAStatusProcessing:
		return tcell.ColorBlue
	case QAStatusCompleted:
		return tcell.ColorGreen
	case QAStatusFailed, QAStatusTimeout:
		return tcell.ColorRed
	default:
		return tcell.ColorWhite
	}
}

// showDirectoryDetails shows details for a selected directory
func (p *AgentsQAPageView) showDirectoryDetails(dirKey string) {
	dir := agentQARegistry.GetDirectory(dirKey)
	if dir == nil {
		p.detailView.SetText("[red]Directory not found[white]")
		return
	}

	// Format the directory detail view
	detail := fmt.Sprintf("[yellow]Directory:[white] %s\n", dirKey)
	detail += fmt.Sprintf("[yellow]Root Directory:[white] %s\n", dir.RootDir)
	detail += fmt.Sprintf("[yellow]Specialty:[white] %s\n", dir.Specialty)
	detail += fmt.Sprintf("[yellow]Created At:[white] %s\n", dir.CreatedAt.Format("15:04:05"))

	if dir.Instruction != "" {
		detail += "\n[yellow]Instructions:[white]\n"
		detail += dir.Instruction + "\n"
	}

	// Count pending questions
	pendingCount := 0
	qas := agentQARegistry.GetQAsByDirectory(dirKey)
	for _, qa := range qas {
		if qa.Status == QAStatusPending {
			pendingCount++
		}
	}

	detail += fmt.Sprintf("\n[yellow]Pending Questions:[white] %d\n", pendingCount)

	// Add system health information
	health := agentQARegistry.GetSystemHealth()
	if directories, ok := health["directories"].([]map[string]any); ok {
		for _, dirHealth := range directories {
			if dirKey, ok := dirHealth["key"].(string); ok && dirKey == dir.Key {
				detail += "\n[yellow]System Health:[white]\n"

				if hasQueue, ok := dirHealth["has_queue"].(bool); ok {
					if hasQueue {
						detail += "✅ Queue: Active\n"
					} else {
						detail += "❌ Queue: Missing\n"
					}
				}

				if hasWaiter, ok := dirHealth["has_waiter"].(bool); ok {
					if hasWaiter {
						detail += "✅ Waiter: Active"
						if waiterName, ok := dirHealth["waiter_name"].(string); ok {
							detail += fmt.Sprintf(" (%s)", waiterName)
						}
						if lastSeen, ok := dirHealth["waiter_last_seen"].(string); ok {
							detail += fmt.Sprintf(" - Last seen: %s", lastSeen)
						}
						if contextCancelled, ok := dirHealth["waiter_context_cancelled"].(bool); ok && contextCancelled {
							detail += " ⚠️ [red]Context Cancelled[white]"
						}
						detail += "\n"
					} else {
						detail += "❌ Waiter: None\n"
					}
				}
				break
			}
		}
	}

	detail += "\n[gray]Specialists can connect at any time to answer questions in this directory[white]\n"

	p.detailView.SetText(detail)
}

// getSpecialistInfo is no longer used since we don't track active specialists
func (p *AgentsQAPageView) getSpecialistInfo(name string) *SpecialistAgent {
	return nil
}

// GetView returns the main view for this page
func (p *AgentsQAPageView) GetView() tview.Primitive {
	return p.view
}
