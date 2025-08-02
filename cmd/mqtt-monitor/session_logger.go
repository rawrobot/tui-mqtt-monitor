package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type SessionLogger struct {
	outputDir   string
	file        *os.File
	maxDuration time.Duration
	startTime   time.Time
	currentTime time.Time
	logger      zerolog.Logger
	mu          sync.Mutex
	closed      bool
	ticker      *time.Ticker
}

func NewSessionLogger(outputDir string, maxDuration time.Duration, logger zerolog.Logger) (*SessionLogger, error) {
	sl := &SessionLogger{
		outputDir:   outputDir,
		maxDuration: maxDuration,
		logger:      logger,
		currentTime: time.Now(),
		ticker:      time.NewTicker(time.Second),
	}

	if err := sl.rotateFile(); err != nil {
		return nil, err
	}

	return sl, nil
}

func (sl *SessionLogger) Start(ctx context.Context) {
	go sl.timeKeeper(ctx)
}

func (sl *SessionLogger) timeKeeper(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-sl.ticker.C:
			sl.mu.Lock()
			sl.currentTime = t
			sl.mu.Unlock()
		}
	}
}

func (sl *SessionLogger) rotateFile() error {
	if sl.file != nil {
		sl.file.Close()
	}

	sl.startTime = sl.currentTime
	filename := sl.generateFilename()
	filepath := filepath.Join(sl.outputDir, filename)

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create session log file: %w", err)
	}

	sl.file = file
	sl.logger.Info().Str("file", filepath).Msg("Created new session log file")

	return nil
}

func (sl *SessionLogger) generateFilename() string {
	return fmt.Sprintf("mqtt_monitor_%s.log", sl.startTime.Format("20060102_150405"))
}

func (sl *SessionLogger) Log(message string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if sl.closed {
		return fmt.Errorf("session logger has been closed")
	}

	if sl.currentTime.Sub(sl.startTime) > sl.maxDuration {
		if err := sl.rotateFile(); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(sl.file, "[%s] %s\n", sl.currentTime.Format("2006-01-02 15:04:05.000"), message)
	return err
}

func (sl *SessionLogger) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if sl.closed {
		return nil
	}

	sl.closed = true
	sl.ticker.Stop()

	if sl.file != nil {
		return sl.file.Close()
	}
	return nil
}
