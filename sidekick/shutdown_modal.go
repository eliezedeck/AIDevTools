package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ShutdownModal displays progress during graceful shutdown
type ShutdownModal struct {
	modal         *tview.Modal
	app           *tview.Application
	mutex         sync.Mutex
	processCount  int
	elapsedTime   time.Duration
	startTime     time.Time
}

// NewShutdownModal creates a new shutdown progress modal
func NewShutdownModal(app *tview.Application) *ShutdownModal {
	modal := tview.NewModal()
	modal.SetBorder(true)
	modal.SetBorderColor(tcell.ColorRed)
	modal.SetBackgroundColor(tcell.ColorBlack)
	modal.SetTextColor(tcell.ColorWhite)
	
	return &ShutdownModal{
		modal:     modal,
		app:       app,
		startTime: time.Now(),
	}
}

// UpdateProgress updates the modal with current shutdown status
func (s *ShutdownModal) UpdateProgress(remainingProcesses int, totalProcesses int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.processCount = remainingProcesses
	s.elapsedTime = time.Since(s.startTime)
	
	// Calculate progress
	terminated := totalProcesses - remainingProcesses
	
	// Format the message
	title := "Shutting Down Sidekick"
	
	var text string
	if remainingProcesses > 0 {
		text = fmt.Sprintf(
			"Gracefully terminating processes...\n\n"+
			"Processes terminated: %d/%d\n"+
			"Remaining: %d\n"+
			"Time elapsed: %.1fs\n\n"+
			"Processes will be force killed in %.1fs",
			terminated, totalProcesses,
			remainingProcesses,
			s.elapsedTime.Seconds(),
			3.0-s.elapsedTime.Seconds(),
		)
	} else {
		text = fmt.Sprintf(
			"All processes terminated successfully!\n\n"+
			"Total processes: %d\n"+
			"Time elapsed: %.1fs\n\n"+
			"Exiting...",
			totalProcesses,
			s.elapsedTime.Seconds(),
		)
	}
	
	s.modal.SetText(text)
	s.modal.SetTitle(title)
	
	// Queue update to the UI
	s.app.QueueUpdateDraw(func() {
		// Modal is already displayed, just needs redraw
	})
}

// Show displays the modal
func (s *ShutdownModal) Show(pages *tview.Pages) {
	s.app.QueueUpdateDraw(func() {
		// Create a centered flex container for the modal
		flex := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(s.modal, 10, 1, true).
				AddItem(nil, 0, 1, false), 60, 1, true).
			AddItem(nil, 0, 1, false)
		
		pages.AddPage("shutdown-modal", flex, true, true)
	})
}

// Hide removes the modal
func (s *ShutdownModal) Hide(pages *tview.Pages) {
	s.app.QueueUpdateDraw(func() {
		pages.RemovePage("shutdown-modal")
	})
}