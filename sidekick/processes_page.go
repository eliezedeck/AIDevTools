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
	treeView      *tview.TreeView
	statusBar     *tview.TextView
	reversedSort  bool
	sessionNodes  map[string]*tview.TreeNode
	processNodes  map[string]*tview.TreeNode
}

// NewProcessesPageView creates a new processes page view
func NewProcessesPageView(tuiApp *TUIApp) *ProcessesPageView {
	p := &ProcessesPageView{
		tuiApp:       tuiApp,
		treeView:     tview.NewTreeView(),
		statusBar:    tview.NewTextView(),
		reversedSort: true, // Default to newest first
		sessionNodes: make(map[string]*tview.TreeNode),
		processNodes: make(map[string]*tview.TreeNode),
	}
	
	p.setupTreeView()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()
	
	return p
}

// setupTreeView configures the tree view
func (p *ProcessesPageView) setupTreeView() {
	p.treeView.SetBorder(true).SetTitle(" Process Sessions ").SetTitleAlign(tview.AlignLeft)
	p.treeView.SetBorderPadding(0, 0, 1, 1)
	
	// Create root node
	root := tview.NewTreeNode("Sessions")
	root.SetColor(tcell.ColorWhite)
	p.treeView.SetRoot(root)
	
	// Set up key handlers
	p.treeView.SetInputCapture(p.handleTreeKeys)
	
	// Set up selection handler
	p.treeView.SetSelectedFunc(p.handleNodeSelected)
}

// setupStatusBar configures the status bar
func (p *ProcessesPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Enter[white]: View Details | [yellow]Space[white]: Expand/Collapse | [yellow]K[white]: Kill Process | [yellow]Del[white]: Remove Process | [yellow]Shift+Del[white]: Remove Session | [yellow]R[white]: Sort | [yellow]Q[white]: Quit")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *ProcessesPageView) setupLayout() {
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.treeView, 0, 1, true).
		AddItem(p.statusBar, 3, 0, false)
}

// handleTreeKeys handles key events for the tree view
func (p *ProcessesPageView) handleTreeKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEnter:
		p.openSelectedItem()
		return nil
	case tcell.KeyDelete:
		if event.Modifiers()&tcell.ModShift != 0 {
			// Shift+Delete: Remove session
			p.removeSelectedSession()
		} else {
			// Delete: Remove process
			p.removeSelectedProcess()
		}
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'k', 'K':
			p.killSelectedProcess()
			return nil
		case 'r', 'R':
			p.toggleSort()
			return nil
		case ' ': // Space key
			p.toggleSelectedNode()
			return nil
		}
	}
	return event
}

// handleNodeSelected handles when a node is selected (Enter key or double-click)
func (p *ProcessesPageView) handleNodeSelected(node *tview.TreeNode) {
	p.openSelectedItem()
}

// openSelectedItem opens the detail view for the selected process or toggles session
func (p *ProcessesPageView) openSelectedItem() {
	node := p.treeView.GetCurrentNode()
	if node == nil {
		return
	}
	
	// Check if this is a process node (has a process ID reference)
	if processID, ok := node.GetReference().(string); ok && processID != "" {
		// This is a process node, open detail view
		p.tuiApp.ShowProcessDetail(processID)
	} else {
		// This is a session node, toggle expansion
		node.SetExpanded(!node.IsExpanded())
	}
}

// toggleSort toggles the sort order (newest first vs oldest first)
func (p *ProcessesPageView) toggleSort() {
	p.reversedSort = !p.reversedSort
	p.Refresh()
}

// Refresh refreshes the processes list
func (p *ProcessesPageView) Refresh() {
	p.populateTreeView()
}

// Update updates the tree view with real-time data
func (p *ProcessesPageView) Update() {
	p.populateTreeView()
}

// populateTreeView populates the tree view with current process data
func (p *ProcessesPageView) populateTreeView() {
	// Clear existing nodes
	root := p.treeView.GetRoot()
	root.ClearChildren()
	p.sessionNodes = make(map[string]*tview.TreeNode)
	p.processNodes = make(map[string]*tview.TreeNode)
	
	// Get processes grouped by session
	sessionGroups := GetProcessesBySession(p.reversedSort)
	
	// Get sorted session names
	sessionNames := make([]string, 0, len(sessionGroups))
	for sessionName := range sessionGroups {
		sessionNames = append(sessionNames, sessionName)
	}
	sort.Strings(sessionNames)
	
	totalProcesses := 0
	totalSessions := 0
	activeSessions := 0
	
	for _, sessionName := range sessionNames {
		processes := sessionGroups[sessionName]
		totalProcesses += len(processes)
		totalSessions++
		
		// Determine session status
		sessionStatus := p.getSessionStatus(processes)
		if sessionStatus == "Active" {
			activeSessions++
		}
		
		// Create session node
		sessionText := fmt.Sprintf("%s (%s) - %d processes", sessionName, sessionStatus, len(processes))
		sessionNode := tview.NewTreeNode(sessionText)
		sessionNode.SetColor(p.getSessionColor(sessionStatus))
		sessionNode.SetExpanded(true) // Default to expanded
		sessionNode.SetReference("") // Session nodes have empty reference
		
		p.sessionNodes[sessionName] = sessionNode
		root.AddChild(sessionNode)
		
		// Add processes for this session
		for _, process := range processes {
			process.Mutex.RLock()
			
			// Create process display text
			processText := p.formatProcessText(process)
			processNode := tview.NewTreeNode(processText)
			processNode.SetColor(getStatusColor(process.Status))
			processNode.SetReference(process.ID) // Store process ID for reference
			
			p.processNodes[process.ID] = processNode
			sessionNode.AddChild(processNode)
			
			process.Mutex.RUnlock()
		}
	}
	
	// Update title with counts and sort order
	sortOrder := "↓ Newest First"
	if !p.reversedSort {
		sortOrder = "↑ Oldest First"
	}
	title := fmt.Sprintf(" Sessions (%d) - %d Active | Processes (%d) - %s ", totalSessions, activeSessions, totalProcesses, sortOrder)
	p.treeView.SetTitle(title)
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

// getSessionColor returns the appropriate color for a session status
func (p *ProcessesPageView) getSessionColor(status string) tcell.Color {
	switch status {
	case "Active":
		return tcell.ColorLime
	case "Inactive":
		return tcell.ColorGray
	default:
		return tcell.ColorWhite
	}
}

// formatProcessText creates display text for a process node
func (p *ProcessesPageView) formatProcessText(process *ProcessTracker) string {
	// Status indicator
	statusSymbol := p.getStatusSymbol(process.Status)
	
	// Format command
	command := process.Command
	if len(process.Args) > 0 {
		command += " " + strings.Join(process.Args, " ")
	}
	if len(command) > 40 {
		command = command[:37] + "..."
	}
	
	// Process name or use command
	name := process.Name
	if name == "" {
		name = process.Command
	}
	if len(name) > 20 {
		name = name[:17] + "..."
	}
	
	// Format start time
	startTime := process.StartTime.Format("15:04:05")
	
	// Create display text
	text := fmt.Sprintf("%s [%s] %s (PID:%d) [%s]", 
		statusSymbol, string(process.Status), name, process.PID, startTime)
	
	return text
}

// getStatusSymbol returns a unicode symbol for the process status
func (p *ProcessesPageView) getStatusSymbol(status ProcessStatus) string {
	switch status {
	case StatusRunning:
		return "▶"
	case StatusCompleted:
		return "✓"
	case StatusFailed:
		return "✗"
	case StatusKilled:
		return "☠"
	case StatusPending:
		return "⏳"
	default:
		return "?"
	}
}

// toggleSelectedNode toggles the expansion of the selected node
func (p *ProcessesPageView) toggleSelectedNode() {
	node := p.treeView.GetCurrentNode()
	if node == nil {
		return
	}
	
	// Only toggle session nodes (those without a process ID reference)
	if node.GetReference() == nil || node.GetReference().(string) == "" {
		node.SetExpanded(!node.IsExpanded())
	}
}

// killSelectedProcess kills the currently selected process
func (p *ProcessesPageView) killSelectedProcess() {
	node := p.treeView.GetCurrentNode()
	if node == nil {
		return
	}
	
	// Check if this is a process node
	if processID, ok := node.GetReference().(string); ok && processID != "" {
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
						tracker.Process.Process.Kill()
					}
				}
				tracker.Status = StatusKilled
				
				// Update display immediately
				p.Refresh()
			}
		}
	}
}

// removeSelectedProcess removes the currently selected process from the registry
func (p *ProcessesPageView) removeSelectedProcess() {
	node := p.treeView.GetCurrentNode()
	if node == nil {
		return
	}
	
	// Check if this is a process node
	if processID, ok := node.GetReference().(string); ok && processID != "" {
		// Remove the process from registry
		registry.removeProcess(processID)
		
		// Update display immediately
		p.Refresh()
	}
}

// removeSelectedSession removes an inactive session and all its processes
func (p *ProcessesPageView) removeSelectedSession() {
	node := p.treeView.GetCurrentNode()
	if node == nil {
		return
	}
	
	// Check if this is a session node (no process ID reference)
	if node.GetReference() == nil || node.GetReference().(string) == "" {
		// Extract session name from node text
		sessionText := node.GetText()
		
		// Find the session name (text before the first space and parentheses)
		sessionName := sessionText
		if spaceIndex := strings.Index(sessionText, " ("); spaceIndex != -1 {
			sessionName = sessionText[:spaceIndex]
		}
		
		// Get processes for this session
		sessionGroups := GetProcessesBySession(false)
		if processes, exists := sessionGroups[sessionName]; exists {
			// Check if session is inactive
			sessionStatus := p.getSessionStatus(processes)
			if sessionStatus == "Inactive" {
				// Remove all processes in this session
				for _, process := range processes {
					registry.removeProcess(process.ID)
				}
				
				// If we have a session manager, remove the session
				if sessionManager != nil && sessionName != "No Session" {
					sessionManager.RemoveSession(sessionName)
				}
				
				// Update display immediately
				p.Refresh()
			}
		}
	}
}

// GetView returns the main view for this page
func (p *ProcessesPageView) GetView() tview.Primitive {
	return p.view
}