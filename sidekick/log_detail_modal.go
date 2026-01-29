package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ShowLogDetailModal displays a full-screen modal with complete log entry details
func ShowLogDetailModal(app *tview.Application, pages *tview.Pages, entry LogEntry) {
	// Create a scrollable text view for the log details
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)

	// Build the full log content
	levelColor := "white"
	switch entry.Level {
	case LogLevelInfo:
		levelColor = "green"
	case LogLevelWarn:
		levelColor = "yellow"
	case LogLevelError:
		levelColor = "red"
	}

	content := fmt.Sprintf(`[yellow]═══════════════════════════════════════════════════════════════════════════════[white]
[yellow]                              LOG ENTRY DETAILS[white]
[yellow]═══════════════════════════════════════════════════════════════════════════════[white]

[yellow]Timestamp:[white]  %s
[yellow]Level:[white]      [%s]%s[white]
[yellow]Source:[white]     %s

[yellow]───────────────────────────────────────────────────────────────────────────────[white]
[yellow]Message:[white]
%s

`,
		entry.Timestamp.Format("2006-01-02 15:04:05.000"),
		levelColor, entry.Level.String(),
		entry.Source,
		entry.Message,
	)

	// Add details if present (this is where the full request dump will be)
	if entry.Details != "" {
		content += fmt.Sprintf(`[yellow]───────────────────────────────────────────────────────────────────────────────[white]
[yellow]Full Details:[white]
%s
`, entry.Details)
	}

	content += `
[yellow]═══════════════════════════════════════════════════════════════════════════════[white]
[grey]Press [yellow]Esc[grey] or [yellow]q[grey] to close | [yellow]↑↓[grey] or [yellow]j/k[grey] to scroll[white]
`

	textView.SetText(content)

	// Style the text view
	textView.SetBorder(true).
		SetTitle(" Log Details (Full) ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorYellow).
		SetBackgroundColor(tcell.ColorBlack)

	// Handle keyboard input
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			pages.RemovePage("log-detail")
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				pages.RemovePage("log-detail")
				return nil
			case 'j':
				// Scroll down
				row, col := textView.GetScrollOffset()
				textView.ScrollTo(row+1, col)
				return nil
			case 'k':
				// Scroll up
				row, col := textView.GetScrollOffset()
				if row > 0 {
					textView.ScrollTo(row-1, col)
				}
				return nil
			case 'g':
				// Go to top
				textView.ScrollToBeginning()
				return nil
			case 'G':
				// Go to bottom
				textView.ScrollToEnd()
				return nil
			}
		case tcell.KeyPgDn:
			row, col := textView.GetScrollOffset()
			textView.ScrollTo(row+20, col)
			return nil
		case tcell.KeyPgUp:
			row, col := textView.GetScrollOffset()
			if row > 20 {
				textView.ScrollTo(row-20, col)
			} else {
				textView.ScrollTo(0, col)
			}
			return nil
		}
		return event
	})

	// Create a full-screen layout with the text view
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(textView, 0, 1, true)

	// Add the modal to pages and show it
	pages.AddAndSwitchToPage("log-detail", flex, true)
	app.SetFocus(textView)
}
