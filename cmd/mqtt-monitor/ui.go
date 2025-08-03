package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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

	// Pool settings with size limits
	InitialBuilderCapacity = 256  // Initial capacity for string builders
	MaxBuilderCapacity     = 1024 // Maximum capacity before discarding
	MaxCacheSize           = 200  // Maximum cache entries before cleanup
	MaxPoolSize            = 100  // Maximum objects to keep in pool
)

var (
	// Pre-compiled regex for better performance
	colorTagRegex   = regexp.MustCompile(`\[[^\]]*\]`)
	multiSpaceRegex = regexp.MustCompile(`\s+`)

	// Pool counters for monitoring
	stringBuilderPoolCount int64
	formatDataPoolCount    int64

	// Object pools with size management
	stringBuilderPool = sync.Pool{
		New: func() interface{} {
			atomic.AddInt64(&stringBuilderPoolCount, 1)
			builder := &strings.Builder{}
			builder.Grow(InitialBuilderCapacity)
			return &pooledStringBuilder{
				Builder: builder,
				maxCap:  MaxBuilderCapacity,
			}
		},
	}

	formatDataPool = sync.Pool{
		New: func() interface{} {
			atomic.AddInt64(&formatDataPoolCount, 1)
			return &formatData{
				timestamp:    make([]byte, 0, 16),
				sourceColor:  make([]byte, 0, 16),
				displayTopic: make([]byte, 0, 64),
				payload:      make([]byte, 0, 256),
			}
		},
	}
)

// pooledStringBuilder wraps strings.Builder with capacity management
type pooledStringBuilder struct {
	*strings.Builder
	maxCap int
}

func (psb *pooledStringBuilder) Reset() {
	psb.Builder.Reset()
	// If the builder has grown too large, don't put it back in the pool
	if psb.Builder.Cap() > psb.maxCap {
		// This will be garbage collected instead of returned to pool
		return
	}
}

// formatData holds reusable byte slices for formatting
type formatData struct {
	timestamp    []byte
	sourceColor  []byte
	displayTopic []byte
	payload      []byte
}

func (fd *formatData) reset() {
	fd.timestamp = fd.timestamp[:0]
	fd.sourceColor = fd.sourceColor[:0]
	fd.displayTopic = fd.displayTopic[:0]
	fd.payload = fd.payload[:0]
}

// shouldReturnToPool checks if the formatData should be returned to pool
func (fd *formatData) shouldReturnToPool() bool {
	// Don't return to pool if any slice has grown too large
	return cap(fd.timestamp) <= 32 &&
		cap(fd.sourceColor) <= 32 &&
		cap(fd.displayTopic) <= 128 &&
		cap(fd.payload) <= 512
}

type UI struct {
	app          *tview.Application
	messagesView *tview.TextView
	errorsView   *tview.TextView
	statusView   *tview.TextView
	flex         *tview.Flex
	messages     []MonitorMessage // Store raw messages for reformatting
	maxMessages  int
	truncate     bool // Whether to truncate messages to fit terminal width

	// Cache for performance
	lastTerminalWidth int
	formatCache       map[string]string // Cache formatted strings
	cacheMutex        sync.RWMutex      // Protect cache access

	// Pool management
	lastPoolCleanup time.Time
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
		app:             app,
		messagesView:    messagesView,
		errorsView:      errorsView,
		statusView:      statusView,
		flex:            flex,
		messages:        make([]MonitorMessage, 0, MaxDisplayedMessages),
		maxMessages:     MaxDisplayedMessages,
		truncate:        truncate,
		formatCache:     make(map[string]string, MaxCacheSize),
		lastPoolCleanup: time.Now(),
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
		case tcell.KeyCtrlL:
			ui.refreshAllMessages()
			return nil
		}
		return event
	})

	// Handle resize events and periodic cleanup
	ui.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		currentWidth := ui.getTerminalWidth()
		if currentWidth != ui.lastTerminalWidth {
			ui.lastTerminalWidth = currentWidth
			ui.clearFormatCache()
		}

		// Periodic pool cleanup (every 30 seconds)
		if time.Since(ui.lastPoolCleanup) > 30*time.Second {
			ui.cleanupPools()
			ui.lastPoolCleanup = time.Now()
		}

		return false
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
	if ui.messagesView == nil {
		return
	}

	// Store the raw message
	ui.messages = append(ui.messages, msg)

	// Keep only the last maxMessages
	if len(ui.messages) > ui.maxMessages {
		copy(ui.messages, ui.messages[1:])
		ui.messages = ui.messages[:ui.maxMessages]
	}

	// Add formatted message to display
	formattedMessage := ui.formatMessageForDisplay(msg)
	ui.app.QueueUpdateDraw(func() {
		fmt.Fprintf(ui.messagesView, "%s\n", formattedMessage)
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

	// Use string builder pool for error formatting
	pooledBuilder := stringBuilderPool.Get().(*pooledStringBuilder)
	defer func() {
		pooledBuilder.Reset()
		// Only return to pool if capacity is reasonable
		if pooledBuilder.Builder.Cap() <= pooledBuilder.maxCap {
			stringBuilderPool.Put(pooledBuilder)
		} else {
			atomic.AddInt64(&stringBuilderPoolCount, -1)
		}
	}()

	builder := pooledBuilder.Builder
	builder.WriteString("[yellow]")
	builder.WriteString(timestamp)
	builder.WriteString("[white] [")
	builder.WriteString(color)
	builder.WriteString("]")
	builder.WriteString(errMsg)
	builder.WriteString("[white]\n")

	formattedErr := builder.String()

	ui.app.QueueUpdateDraw(func() {
		fmt.Fprint(ui.errorsView, formattedErr)
		ui.errorsView.ScrollToEnd()
	})
}

func (ui *UI) UpdateStatus(status string) {
	ui.app.QueueUpdateDraw(func() {
		ui.statusView.Clear()
		// Add pool statistics to status for monitoring
		poolStats := fmt.Sprintf(" | Pools: SB=%d FD=%d",
			atomic.LoadInt64(&stringBuilderPoolCount),
			atomic.LoadInt64(&formatDataPoolCount))
		fmt.Fprintf(ui.statusView, " %s%s | Press Ctrl+C or Esc to quit | Tab to switch views", status, poolStats)
	})
}

func (ui *UI) getTerminalWidth() int {
	if ui.messagesView != nil {
		_, _, width, _ := ui.messagesView.GetInnerRect()
		if width > 10 {
			return width
		}

		_, _, width, _ = ui.messagesView.GetRect()
		if width > 10 {
			return width - 4
		}
	}

	if ui.flex != nil {
		_, _, width, _ := ui.flex.GetRect()
		if width > 10 {
			return width - 4
		}
	}

	return 120
}

func (ui *UI) formatMessageForDisplay(msg MonitorMessage) string {
	// Create cache key for this message format
	terminalWidth := ui.getTerminalWidth()

	// Use string builder pool for cache key creation
	keyBuilder := stringBuilderPool.Get().(*pooledStringBuilder)
	defer func() {
		keyBuilder.Reset()
		// Only return to pool if capacity is reasonable
		if keyBuilder.Builder.Cap() <= keyBuilder.maxCap {
			stringBuilderPool.Put(keyBuilder)
		} else {
			atomic.AddInt64(&stringBuilderPoolCount, -1)
		}
	}()

	keyBuilder.Builder.WriteString(msg.Source)
	keyBuilder.Builder.WriteByte('|')
	keyBuilder.Builder.WriteString(msg.DisplayTopic)
	keyBuilder.Builder.WriteByte('|')
	keyBuilder.Builder.WriteString(msg.Payload)
	keyBuilder.Builder.WriteByte('|')
	if ui.truncate {
		keyBuilder.Builder.WriteString("t")
	} else {
		keyBuilder.Builder.WriteString("f")
	}
	keyBuilder.Builder.WriteByte('|')
	keyBuilder.Builder.WriteString(fmt.Sprintf("%d", terminalWidth))

	cacheKey := keyBuilder.Builder.String()

	// Check cache first with read lock
	ui.cacheMutex.RLock()
	if cached, exists := ui.formatCache[cacheKey]; exists {
		ui.cacheMutex.RUnlock()
		return cached
	}
	ui.cacheMutex.RUnlock()

	var result string

	// If truncation is disabled, use a simple format without width calculations
	if !ui.truncate {
		result = ui.formatWithoutTruncation(msg)
	} else {
		result = ui.formatWithTruncation(msg)
	}

	// Cache the result with write lock
	ui.cacheMutex.Lock()
	// Check cache size and clean if necessary
	if len(ui.formatCache) >= MaxCacheSize {
		// Clear half the cache (simple LRU approximation)
		count := 0
		for k := range ui.formatCache {
			delete(ui.formatCache, k)
			count++
			if count >= MaxCacheSize/2 {
				break
			}
		}
	}
	ui.formatCache[cacheKey] = result
	ui.cacheMutex.Unlock()

	return result
}

func (ui *UI) formatWithoutTruncation(msg MonitorMessage) string {
	timestamp := msg.Timestamp.Format("15:04:05.000")
	sourceColor := getSourceColor(msg.Color)

	return fmt.Sprintf("[yellow]%s[white] [%s]%s[white] [green]%s[white] %s",
		timestamp, sourceColor, msg.Source, msg.DisplayTopic, msg.Payload)
}

func (ui *UI) formatWithTruncation(msg MonitorMessage) string {
	maxWidth := ui.getTerminalWidth()
	if maxWidth < 50 {
		maxWidth = 120
	}

	displaySource := truncateTextIfNeeded(msg.Source, MaxSourceDisplayWidth, TruncatedSourceWidth)
	displayTopic := truncateTextIfNeeded(msg.DisplayTopic, MaxTopicDisplayWidth, TruncatedTopicWidth)
	sourceColor := getSourceColor(msg.Color)

	timestamp := msg.Timestamp.Format("15:04:05.000")
	prefix := fmt.Sprintf("[yellow]%s[white] [%s]%s[white] [green]%s[white] ",
		timestamp, sourceColor, displaySource, displayTopic)

	visiblePrefixLength := getVisibleLengthOptimized(prefix)
	availableForPayload := maxWidth - visiblePrefixLength

	if availableForPayload < MinimumPayloadWidth {
		availableForPayload = MinimumPayloadWidth
	}

	cleanPayload := cleanPayloadTextOptimized(msg.Payload)
	truncatedPayload := truncateText(cleanPayload, availableForPayload)

	return prefix + truncatedPayload
}

func (ui *UI) refreshAllMessages() {
	if ui.messagesView == nil {
		return
	}

	ui.app.QueueUpdateDraw(func() {
		ui.messagesView.Clear()
		// Use strings.Builder for better performance when concatenating many strings
		builder := stringBuilderPool.Get().(*pooledStringBuilder)
		defer func() {
			builder.Reset()
			// Only return to pool if capacity is reasonable
			if builder.Builder.Cap() <= builder.maxCap {
				stringBuilderPool.Put(builder)
			} else {
				atomic.AddInt64(&stringBuilderPoolCount, -1)
			}
		}()
		builder.Builder.Grow(len(ui.messages) * 100) // Pre-allocate approximate space

		for _, msg := range ui.messages {
			formattedMessage := ui.formatMessageForDisplay(msg)
			builder.Builder.WriteString(formattedMessage)
			builder.Builder.WriteByte('\n')
		}

		fmt.Fprint(ui.messagesView, builder.Builder.String())
		ui.messagesView.ScrollToEnd()
	})
}

func (ui *UI) clearFormatCache() {
	// Clear the format cache when terminal width changes
	ui.cacheMutex.Lock()
	for k := range ui.formatCache {
		delete(ui.formatCache, k)
	}
	ui.cacheMutex.Unlock()
}

func (ui *UI) cleanupPools() {
	// Cleanup stringBuilderPool
	for i := 0; i < MaxPoolSize; i++ {
		if pooledBuilder, ok := stringBuilderPool.Get().(*pooledStringBuilder); ok {
			if pooledBuilder.Builder.Cap() > pooledBuilder.maxCap {
				atomic.AddInt64(&stringBuilderPoolCount, -1)
				continue
			}
			pooledBuilder.Reset()
			stringBuilderPool.Put(pooledBuilder)
		} else {
			break
		}
	}

	// Cleanup formatDataPool
	for i := 0; i < MaxPoolSize; i++ {
		if fd, ok := formatDataPool.Get().(*formatData); ok {
			if !fd.shouldReturnToPool() {
				atomic.AddInt64(&formatDataPoolCount, -1)
				continue
			}
			fd.reset()
			formatDataPool.Put(fd)
		} else {
			break
		}
	}
}

// Optimized helper functions

func getSourceColor(color string) string {
	if color != "" {
		return color
	}
	return "cyan"
}

func truncateTextIfNeeded(text string, maxWidth, truncatedWidth int) string {
	if len(text) > maxWidth {
		return truncateText(text, truncatedWidth)
	}
	return text
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

func cleanPayloadTextOptimized(payload string) string {
	// Remove characters that could break display formatting using regex
	cleaned := multiSpaceRegex.ReplaceAllString(payload, " ")
	return strings.TrimSpace(cleaned)
}

func getVisibleLengthOptimized(text string) int {
	// Remove all tview color tags to get actual visible length using regex
	return len(colorTagRegex.ReplaceAllString(text, ""))
}
