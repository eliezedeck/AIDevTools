package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ShowKillConfirmation displays a confirmation dialog before killing a process
func ShowKillConfirmation(app *tview.Application, pages *tview.Pages, processName string, onConfirm func()) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to kill this process?\n\n%s\n\nThis action cannot be undone.", processName)).
		AddButtons([]string{"Kill", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Remove the modal
			pages.RemovePage("kill-confirmation")
			
			if buttonIndex == 0 { // "Kill" was selected
				onConfirm()
			}
			// If "Cancel" was selected or Esc pressed, just return to the app
		})
	
	// Style the modal
	modal.SetBorder(true).
		SetBorderColor(tcell.ColorRed).
		SetBackgroundColor(tcell.ColorBlack)
	
	// Set keyboard shortcuts
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'k', 'K':
				// Trigger Kill button
				pages.RemovePage("kill-confirmation")
				onConfirm()
				return nil
			case 'c', 'C', 'n', 'N', 'q', 'Q':
				// Trigger Cancel button
				pages.RemovePage("kill-confirmation")
				return nil
			}
		case tcell.KeyEsc:
			// Cancel - same as Cancel button
			pages.RemovePage("kill-confirmation")
			return nil
		}
		return event
	})
	
	// Create a centered flex container for the modal
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modal, 8, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)
	
	// Add the modal to pages and show it
	pages.AddAndSwitchToPage("kill-confirmation", flex, true)
	app.SetFocus(modal)
}
