package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UI struct {
	app          *tview.Application
	messagesView *tview.TextView
	errorsView   *tview.TextView
	statusView   *tview.TextView
	flex         *tview.Flex
}

func NewUI() *UI {
	app := tview.NewApplication()

	// Messages view (main area)
	messagesView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(10000) // Buffer size
	messagesView.SetBorder(true).SetTitle(" Messages ")

	// Errors/Status view (bottom area)
	errorsView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(1000)
	errorsView.SetBorder(true).SetTitle(" Connection Status & Errors ")

	// Status bar
	statusView := tview.NewTextView().
		SetDynamicColors(true)
	statusView.SetBorder(true).SetTitle(" Status ")

	// Layout
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(messagesView, 0, 3, true).
		AddItem(errorsView, 0, 1, false).
		AddItem(statusView, 3, 0, false)

	return &UI{
		app:          app,
		messagesView: messagesView,
		errorsView:   errorsView,
		statusView:   statusView,
		flex:         flex,
	}
}

func (ui *UI) Start(ctx context.Context) error {
	ui.app.SetRoot(ui.flex, true)

	// Key bindings
	ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			// Stop the application immediately
			ui.app.Stop()
			return nil
		case tcell.KeyEscape:
			// Alternative way to quit
			ui.app.Stop()
			return nil
		case tcell.KeyTab:
			// Switch focus between views
			if ui.app.GetFocus() == ui.messagesView {
				ui.app.SetFocus(ui.errorsView)
			} else {
				ui.app.SetFocus(ui.messagesView)
			}
			return nil
		}
		return event
	})

	// Monitor context for cancellation
	go func() {
		<-ctx.Done()
		ui.app.QueueUpdateDraw(func() {
			ui.app.Stop()
		})
	}()

	// Run the application
	return ui.app.Run()
}

func (ui *UI) Stop() {
	// Force stop the application
	go func() {
		time.Sleep(10 * time.Millisecond) // Small delay to ensure proper cleanup
		ui.app.Stop()
	}()
}

func (ui *UI) AddMessage(msg MonitorMessage) {
	timestamp := msg.Timestamp.Format("15:04:05.000") // Changed: only time, no date

	// Format the message with colors - use brackets around color names
	formattedMsg := fmt.Sprintf("[yellow]%s[white] [%s]%s [green]%s[white] %s\n",
		timestamp,
		msg.Color,  // Use the client's color
		msg.Source, // Display the actual source name (core2, vptu, etc.)
		msg.DisplayTopic,
		msg.Payload)

	ui.app.QueueUpdateDraw(func() {
		fmt.Fprint(ui.messagesView, formattedMsg)
		ui.messagesView.ScrollToEnd()
	})
}

func (ui *UI) AddError(err error) {
	timestamp := time.Now().Format("15:04:05.000")

	errMsg := err.Error()
	var color string
	if strings.Contains(errMsg, "connected") || strings.Contains(errMsg, "subscribed") {
		color = "green"
	} else {
		color = "red"
	}

	formattedErr := fmt.Sprintf("[yellow]%s[white] [%s]%s[white]\n",
		timestamp, color, errMsg)

	ui.app.QueueUpdateDraw(func() {
		fmt.Fprint(ui.errorsView, formattedErr)
		ui.errorsView.ScrollToEnd()
	})
}

func (ui *UI) UpdateStatus(status string) {
	ui.app.QueueUpdateDraw(func() {
		ui.statusView.Clear()
		fmt.Fprintf(ui.statusView, " %s | Press Ctrl+C or Esc to quit | Tab to switch views", status)
	})
}
