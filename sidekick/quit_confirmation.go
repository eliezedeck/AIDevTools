package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ShowQuitConfirmation displays a confirmation dialog before quitting
func ShowQuitConfirmation(app *tview.Application, pages *tview.Pages, onConfirm func()) {
	modal := tview.NewModal().
		SetText("Are you sure you want to quit Sidekick?\n\nAll managed processes will be terminated.").
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Remove the modal
			pages.RemovePage("quit-confirmation")
			
			if buttonIndex == 0 { // "Yes" was selected
				onConfirm()
			}
			// If "No" was selected or Esc pressed, just return to the app
		})
	
	// Style the modal
	modal.SetBorder(true).
		SetBorderColor(tcell.ColorYellow).
		SetBackgroundColor(tcell.ColorBlack)
	
	// Set keyboard shortcuts
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'y', 'Y':
				// Trigger Yes button
				pages.RemovePage("quit-confirmation")
				onConfirm()
				return nil
			case 'n', 'N', 'q', 'Q':
				// Trigger No button
				pages.RemovePage("quit-confirmation")
				return nil
			}
		case tcell.KeyEsc:
			// Cancel - same as No
			pages.RemovePage("quit-confirmation")
			return nil
		}
		return event
	})
	
	// Create a centered flex container for the modal
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modal, 7, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)
	
	// Add the modal to pages and show it
	pages.AddAndSwitchToPage("quit-confirmation", flex, true)
	app.SetFocus(modal)
}