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
	MinimumDisplayWidth    = 40
	MinimumPayloadWidth    = 10
	AbsoluteMinimumPayload = 5

	// UI Element Spacing
	BorderWidth          = 4 // left and right borders (2 chars each)
	InternalPadding      = 2 // internal padding
	ScrollbarWidth       = 1 // scrollbar if present
	SpaceBetweenElements = 1 // space between timestamp, source, topic, payload

	// Text Truncation
	EllipsisLength        = 3  // length of "..."
	MaxTopicDisplayWidth  = 25 // maximum width for topic before truncation
	MaxSourceDisplayWidth = 12 // maximum width for source before truncation
	TruncatedTopicWidth   = 22 // topic width after truncation (25 - 3 for "...")
	TruncatedSourceWidth  = 9  // source width after truncation (12 - 3 for "...")

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
	truncate     bool // Whether to truncate messages to fit terminal width
}

func NewUI(truncate bool) *UI {
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
		truncate:     truncate,
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
		case tcell.KeyCtrlL: // Add Ctrl+L to refresh display
			ui.refreshAllMessages()
			return nil
		}
		return event
	})

	// Handle resize events
	ui.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// This will be called before each draw, allowing us to handle resizes
		return false // Return false to continue with normal drawing
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
	// Try multiple methods to get the width

	// Method 1: Get from messages view inner rect
	if ui.messagesView != nil {
		_, _, width, _ := ui.messagesView.GetInnerRect()
		if width > 10 { // Make sure we got a reasonable width
			return width
		}

		// Method 2: Get from messages view rect (includes borders)
		_, _, width, _ = ui.messagesView.GetRect()
		if width > 10 {
			return width - 4 // Subtract border width
		}
	}

	// Method 3: Get from flex container
	if ui.flex != nil {
		_, _, width, _ := ui.flex.GetRect()
		if width > 10 {
			return width - 4 // Subtract border width
		}
	}

	// Fallback to a reasonable default
	return 120 // Increase default since modern terminals are usually wider
}

func (ui *UI) formatMessageForDisplay(msg MonitorMessage) string {
	// If truncation is disabled, use a simple format without width calculations
	if !ui.truncate {
		timestamp := msg.Timestamp.Format("15:04:05.000")
		sourceColor := "cyan"
		if msg.Color != "" {
			sourceColor = msg.Color
		}

		return fmt.Sprintf("[yellow]%s[white] [%s]%s[white] [green]%s[white] %s",
			timestamp,
			sourceColor,
			msg.Source,
			msg.DisplayTopic,
			msg.Payload)
	}

	// Original truncation logic for when truncate is enabled
	maxWidth := ui.getTerminalWidth()

	if maxWidth < 50 {
		maxWidth = 120
	}

	displaySource := msg.Source
	displayTopic := msg.DisplayTopic

	if len(displaySource) > MaxSourceDisplayWidth {
		displaySource = truncateText(displaySource, TruncatedSourceWidth)
	}

	if len(displayTopic) > MaxTopicDisplayWidth {
		displayTopic = truncateText(displayTopic, TruncatedTopicWidth)
	}

	sourceColor := "cyan"
	if msg.Color != "" {
		sourceColor = msg.Color
	}

	timestamp := msg.Timestamp.Format("15:04:05.000")
	prefix := fmt.Sprintf("[yellow]%s[white] [%s]%s[white] [green]%s[white] ",
		timestamp,
		sourceColor,
		displaySource,
		displayTopic)

	visiblePrefixLength := getVisibleLength(prefix)
	availableForPayload := maxWidth - visiblePrefixLength

	if availableForPayload < MinimumPayloadWidth {
		availableForPayload = MinimumPayloadWidth
	}

	cleanPayload := cleanPayloadText(msg.Payload)
	truncatedPayload := truncateText(cleanPayload, availableForPayload)

	return prefix + truncatedPayload
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

func getVisibleLength(text string) int {
	// Remove all tview color tags to get actual visible length
	result := text
	for {
		start := strings.Index(result, "[")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "]")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	return len(result)
}
