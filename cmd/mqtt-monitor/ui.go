package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	// UI Layout Constants
	DefaultTerminalWidth   = 80
	MinimumDisplayWidth    = 20
	MinimumPayloadWidth    = 5
	AbsoluteMinimumPayload = 5

	// UI Element Spacing
	BorderWidth          = 4 // left and right borders (2 chars each)
	InternalPadding      = 2 // internal padding
	ScrollbarWidth       = 1 // scrollbar if present
	SpaceBetweenElements = 1 // space between timestamp, source, topic, payload

	// Text Truncation
	EllipsisLength        = 3  // length of "..."
	MaxTopicDisplayWidth  = 20 // maximum width for topic before truncation
	MaxSourceDisplayWidth = 15 // maximum width for source before truncation
	TruncatedTopicWidth   = 17 // topic width after truncation (20 - 3 for "...")
	TruncatedSourceWidth  = 12 // source width after truncation (15 - 3 for "...")

	// Timestamp formatting
	TimestampFormatWidth = 12 // "15:04:05.000" + space = 12 chars

	// Performance settings
	MaxDisplayedMessages = 1000 // maximum messages to keep in display
)

type UI struct {
	app          *tview.Application
	messagesView *tview.TextView
	errorsView   *tview.TextView
	statusView   *tview.TextView
	flex         *tview.Flex
	messages     []MonitorMessage // Store raw messages for reformatting
	maxMessages  int
}

func NewUI() *UI {
	app := tview.NewApplication()

	// Messages view (main area)
	messagesView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(MaxDisplayedMessages)
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
		messages:     make([]MonitorMessage, 0),
		maxMessages:  MaxDisplayedMessages,
	}
}

func (ui *UI) Start(ctx context.Context) error {
	ui.app.SetRoot(ui.flex, true)

	// Key bindings
	ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			ui.app.Stop()
			return nil
		case tcell.KeyEscape:
			ui.app.Stop()
			return nil
		case tcell.KeyTab:
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

	return ui.app.Run()
}

func (ui *UI) Stop() {
	go func() {
		time.Sleep(10 * time.Millisecond)
		ui.app.Stop()
	}()
}

func (ui *UI) AddMessage(msg MonitorMessage) {
	if ui.messagesView != nil {
		// Store the raw message
		ui.messages = append(ui.messages, msg)

		// Keep only the last maxMessages
		if len(ui.messages) > ui.maxMessages {
			ui.messages = ui.messages[1:]
		}

		// Add formatted message to display
		formattedMessage := ui.formatMessageForDisplay(msg)
		ui.app.QueueUpdateDraw(func() {
			fmt.Fprintf(ui.messagesView, "%s\n", formattedMessage)
			ui.messagesView.ScrollToEnd()
		})
	}
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

func (ui *UI) getTerminalWidth() int {
	// Get width from the messages view
	if ui.messagesView != nil {
		_, _, width, _ := ui.messagesView.GetInnerRect()
		if width > 0 {
			// The inner rect already accounts for borders, so use it directly
			if width < MinimumDisplayWidth {
				width = MinimumDisplayWidth
			}
			return width
		}
	}

	return DefaultTerminalWidth
}

func (ui *UI) formatMessageForDisplay(msg MonitorMessage) string {
	maxWidth := ui.getTerminalWidth()

	// Calculate space used by fixed elements
	timestamp := msg.Timestamp.Format("15:04:05.000")
	timestampWidth := len(timestamp) + SpaceBetweenElements

	sourceWidth := len(msg.Source) + SpaceBetweenElements
	topicWidth := len(msg.DisplayTopic) + SpaceBetweenElements

	fixedWidth := timestampWidth + sourceWidth + topicWidth
	availableForPayload := maxWidth - fixedWidth

	// Ensure minimum space for payload by truncating other elements if needed
	displaySource := msg.Source
	displayTopic := msg.DisplayTopic

	if availableForPayload < MinimumPayloadWidth {
		// Truncate topic first if it's too long
		if len(msg.DisplayTopic) > MaxTopicDisplayWidth {
			displayTopic = truncateText(msg.DisplayTopic, TruncatedTopicWidth)
			topicWidth = MaxTopicDisplayWidth
		}

		// Truncate source if still not enough space
		if len(msg.Source) > MaxSourceDisplayWidth && availableForPayload < MinimumPayloadWidth {
			displaySource = truncateText(msg.Source, TruncatedSourceWidth)
			sourceWidth = MaxSourceDisplayWidth
		}

		// Recalculate available space
		fixedWidth = timestampWidth + sourceWidth + topicWidth
		availableForPayload = maxWidth - fixedWidth

		if availableForPayload < AbsoluteMinimumPayload {
			availableForPayload = AbsoluteMinimumPayload
		}
	}

	// Clean and truncate payload
	cleanPayload := cleanPayloadText(msg.Payload)
	truncatedPayload := truncateText(cleanPayload, availableForPayload)

	// Use the client's assigned color for the source name
	sourceColor := "cyan" // default color
	if msg.Color != "" {
		sourceColor = msg.Color
	}

	return fmt.Sprintf("[yellow]%s[white] [%s]%s[white] [green]%s[white] [white]%s[white]",
		timestamp,
		sourceColor,
		displaySource,
		displayTopic,
		truncatedPayload)
}

func (ui *UI) refreshAllMessages() {
	if ui.messagesView != nil {
		ui.app.QueueUpdateDraw(func() {
			ui.messagesView.Clear()
			for _, msg := range ui.messages {
				formattedMessage := ui.formatMessageForDisplay(msg)
				fmt.Fprintf(ui.messagesView, "%s\n", formattedMessage)
			}
			ui.messagesView.ScrollToEnd()
		})
	}
}

func truncateText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if len(text) <= maxWidth {
		return text
	}
	if maxWidth <= EllipsisLength {
		return text[:maxWidth]
	}
	return text[:maxWidth-EllipsisLength] + "..."
}

func cleanPayloadText(payload string) string {
	// Remove characters that could break display formatting
	cleaned := strings.ReplaceAll(payload, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = strings.ReplaceAll(cleaned, "\t", " ")

	// Collapse multiple spaces into single spaces
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}

	return strings.TrimSpace(cleaned)
}