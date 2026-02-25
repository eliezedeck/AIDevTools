package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// FeaturesPageView represents the features/settings page
type FeaturesPageView struct {
	tuiApp       *TUIApp
	view         *tview.Flex
	featuresList *tview.List
	statusBar    *tview.TextView
	webhookInput *tview.InputField
	inputVisible bool
}

// NewFeaturesPageView creates a new features page view
func NewFeaturesPageView(tuiApp *TUIApp) *FeaturesPageView {
	p := &FeaturesPageView{
		tuiApp:       tuiApp,
		featuresList: tview.NewList(),
		statusBar:    tview.NewTextView(),
		webhookInput: tview.NewInputField(),
	}

	p.setupList()
	p.setupWebhookInput()
	p.setupStatusBar()
	p.setupLayout()
	p.Refresh()

	return p
}

// setupList configures the features list
func (p *FeaturesPageView) setupList() {
	p.featuresList.SetBorder(true).SetTitle(" Features ").SetTitleAlign(tview.AlignLeft)
	p.featuresList.SetBorderPadding(1, 1, 2, 2)
	p.featuresList.ShowSecondaryText(true)
	p.featuresList.SetHighlightFullLine(true)
	p.featuresList.SetSelectedBackgroundColor(tcell.ColorDarkSlateGray)
	p.featuresList.SetMainTextStyle(tcell.StyleDefault.Bold(true))

	p.featuresList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			p.toggleSelectedFeature()
			return nil
		}
		return event
	})
}

// setupWebhookInput configures the Discord webhook URL input field
func (p *FeaturesPageView) setupWebhookInput() {
	p.webhookInput.SetBorder(true).SetTitle(" Discord Webhook URL ").SetTitleAlign(tview.AlignLeft)
	p.webhookInput.SetBorderPadding(0, 0, 1, 1)
	p.webhookInput.SetFieldBackgroundColor(tcell.ColorDarkSlateGray)
	p.webhookInput.SetPlaceholder("https://discord.com/api/webhooks/...")
	p.webhookInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			url := p.webhookInput.GetText()
			if url == "" {
				// Empty input clears the webhook
				if err := ClearDiscordWebhookURL(); err != nil {
					LogError("Features", "Failed to clear Discord webhook", err.Error())
				}
			} else {
				if err := SetDiscordWebhookURL(url); err != nil {
					LogError("Features", "Failed to set Discord webhook", err.Error())
					// Show error briefly — the list will refresh with the old state
				}
			}
			p.hideWebhookInput()
			p.Refresh()
		case tcell.KeyEsc:
			p.hideWebhookInput()
		}
	})
}

// setupStatusBar configures the status bar
func (p *FeaturesPageView) setupStatusBar() {
	p.statusBar.SetBorder(true).SetTitle(" Controls ").SetTitleAlign(tview.AlignLeft)
	p.statusBar.SetText("[yellow]↑↓[white]: Navigate | [yellow]Enter[white]: Toggle/Edit | [yellow]Esc[white]: Back | [yellow]Q[white]: Quit\n[grey]Pages: [yellow]1[white]: Processes | [yellow]2[white]: Notifications | [yellow]3[white]: Agents Q&A | [yellow]4[white]: Logs | [yellow]5[white]: Features[grey]")
	p.statusBar.SetTextAlign(tview.AlignCenter)
	p.statusBar.SetDynamicColors(true)
}

// setupLayout creates the main layout
func (p *FeaturesPageView) setupLayout() {
	p.view = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.featuresList, 0, 1, true).
		AddItem(p.statusBar, 4, 0, false)
}

// showWebhookInput shows the webhook URL input field
func (p *FeaturesPageView) showWebhookInput() {
	if p.inputVisible {
		return
	}
	p.inputVisible = true

	// Pre-fill with current URL if set
	cfg, err := LoadConfig()
	if err == nil && cfg.Discord.WebhookURL != "" {
		p.webhookInput.SetText(cfg.Discord.WebhookURL)
	} else {
		p.webhookInput.SetText("")
	}

	// Rebuild layout with input field
	p.view.Clear()
	p.view.AddItem(p.featuresList, 0, 1, false)
	p.view.AddItem(p.webhookInput, 3, 0, true)
	p.view.AddItem(p.statusBar, 4, 0, false)

	p.tuiApp.app.SetFocus(p.webhookInput)
}

// hideWebhookInput hides the webhook URL input field
func (p *FeaturesPageView) hideWebhookInput() {
	if !p.inputVisible {
		return
	}
	p.inputVisible = false

	// Rebuild layout without input field
	p.view.Clear()
	p.view.AddItem(p.featuresList, 0, 1, true)
	p.view.AddItem(p.statusBar, 4, 0, false)

	p.tuiApp.app.SetFocus(p.featuresList)
}

// toggleSelectedFeature toggles the currently selected feature
func (p *FeaturesPageView) toggleSelectedFeature() {
	idx := p.featuresList.GetCurrentItem()

	switch idx {
	case 0: // Cursor Keybindings Watcher
		if IsKeybindingsWatcherEnabled() {
			if err := DisableKeybindingsWatcher(); err != nil {
				LogError("Features", "Failed to disable keybindings watcher", err.Error())
			}
		} else {
			if err := EnableKeybindingsWatcher(); err != nil {
				LogError("Features", "Failed to enable keybindings watcher", err.Error())
			}
		}
		p.Refresh()

	case 1: // Discord Webhook
		p.showWebhookInput()
	}
}

// Refresh refreshes the features list with current state
func (p *FeaturesPageView) Refresh() {
	p.featuresList.Clear()

	// Feature 1: Cursor Keybindings Watcher
	watcherEnabled := IsKeybindingsWatcherEnabled()
	watcherStatus := "[red]OFF"
	if watcherEnabled {
		watcherStatus = "[green]ON"
	}
	watcherLabel := fmt.Sprintf("Cursor Shift+Enter Fix  %s", watcherStatus)
	watcherDesc := "Ensure Cursor always has the correct shift+enter binding for Claude Code's terminal"
	p.featuresList.AddItem(watcherLabel, watcherDesc, 0, nil)

	// Feature 2: Discord Webhook Notifications
	discordConfigured := IsDiscordWebhookConfigured()
	discordStatus := "[red]OFF"
	discordDesc := "Send notifications to Discord via webhook (press Enter to configure)"
	if discordConfigured {
		discordStatus = "[green]ON"
		masked := GetDiscordWebhookURLMasked()
		discordDesc = fmt.Sprintf("Notifications sent to Discord (%s) — press Enter to change", masked)
	}
	discordLabel := fmt.Sprintf("Discord Webhook         %s", discordStatus)
	p.featuresList.AddItem(discordLabel, discordDesc, 0, nil)
}

// Update refreshes the page (called by the TUI update loop)
func (p *FeaturesPageView) Update() {
	// Features page is mostly static — only refresh if visible
}

// GetView returns the main view for this page
func (p *FeaturesPageView) GetView() tview.Primitive {
	return p.view
}
