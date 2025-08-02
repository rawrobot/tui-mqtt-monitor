package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	gitHash   string
	buildDate string
)

func main() {
	// Configure zerolog before loading configuration
	configureZerolog()

	config := loadConfiguration()
	if config == nil {
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionLogger := initializeSessionLogger(config)
	if sessionLogger != nil {
		sessionLogger.Start(ctx)
		defer sessionLogger.Close()
	}

	ui := NewUI()
	messagesCh, errorsCh := make(chan MonitorMessage, 1000), make(chan error, 100)
	clients := createMQTTClients(config, messagesCh, errorsCh, ctx)

	sigCh := setupSignalHandler()
	uiDone := startUI(ui, ctx)

	connectClients(clients, errorsCh, ctx)

	messageHandlerDone := handleMessagesAndErrors(ui, messagesCh, errorsCh, clients, sessionLogger, ctx)

	shutdownReason := waitForShutdownSignal(sigCh, uiDone)
	performGracefulShutdown(cancel, ui, clients, messageHandlerDone, messagesCh, errorsCh, shutdownReason)
}

func configureZerolog() {
	// Disable console output when TUI is running
	// Use a discard writer to suppress all console output
	log.Logger = zerolog.New(io.Discard).With().Timestamp().Logger()
	
	// Set global log level
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

func loadConfiguration() *Config {
	configFile := flag.String("config", "config.toml", "Path to configuration file")
	versionFlag := flag.Bool("version", false, "Display version information")

	// Override default usage function
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nBuild Information:\n")
		fmt.Fprintf(os.Stderr, "  Build Date: %s\n", buildDate)
		fmt.Fprintf(os.Stderr, "  Git Hash: %s\n", gitHash)
	}

	flag.Parse()

	// Check if version flag is set
	if *versionFlag {
		fmt.Printf("Build Date: %s\nGit Hash: %s\n", buildDate, gitHash)
		os.Exit(0)
	}

	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	if len(config.Connections) == 0 {
		log.Fatal().Msg("No connections configured")
	}

	// Configure zerolog based on config
	configureZerologFromConfig(config)

	return config
}

func configureZerologFromConfig(config *Config) {
	// Parse log level
	var level zerolog.Level
	switch config.Logging.Level {
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	default:
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)

	// Always use discard writer when TUI is running
	// Only log to session files, not console
	log.Logger = zerolog.New(io.Discard).With().Timestamp().Logger()
}

func initializeSessionLogger(config *Config) *SessionLogger {
	if !config.Logging.EnableSessionLog {
		return nil
	}

	sessionLogMaxDuration, err := time.ParseDuration(config.Logging.SessionLogMaxDuration)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid session_log_max_duration")
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(config.Logging.OutputDir, 0755); err != nil {
		log.Error().Err(err).Msg("Failed to create log output directory")
		return nil
	}

	sessionLogger, err := NewSessionLogger(config.Logging.OutputDir, sessionLogMaxDuration, log.Logger)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize session logger")
		return nil
	}

	return sessionLogger
}

func createMQTTClients(config *Config, messagesCh chan MonitorMessage, errorsCh chan error, ctx context.Context) []*MQTTClient {
	var clients []*MQTTClient
	// Define colors for different clients
	colors := []string{"green", "blue", "yellow", "magenta", "cyan", "white", "orange", "purple", "brown", "red"}

	for i, connConfig := range config.Connections {
		client := NewMQTTClient(connConfig, messagesCh, errorsCh, config.Display.TopicDepth)
		client.SetContext(ctx)
		// Assign color cyclically
		client.SetColor(colors[i%len(colors)])
		clients = append(clients, client)
	}
	return clients
}

func setupSignalHandler() chan os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	return sigCh
}

func startUI(ui *UI, ctx context.Context) chan error {
	uiDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				uiDone <- fmt.Errorf("UI panic: %v", r)
			}
		}()
		uiDone <- ui.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond) // Give UI time to initialize
	return uiDone
}

func connectClients(clients []*MQTTClient, errorsCh chan error, ctx context.Context) {
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *MQTTClient) {
			defer wg.Done()
			if err := c.Connect(); err != nil {
				select {
				case errorsCh <- fmt.Errorf("failed to connect %s: %w", c.name, err):
				case <-ctx.Done():
				}
			}
		}(client)
	}
}

func handleMessagesAndErrors(ui *UI, messagesCh chan MonitorMessage, errorsCh chan error, clients []*MQTTClient, sessionLogger *SessionLogger, ctx context.Context) chan struct{} {
	messageHandlerDone := make(chan struct{})
	go func() {
		defer close(messageHandlerDone)
		messageCount, errorCount := 0, 0

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-messagesCh:
				if !ok {
					return
				}
				handleMessage(ui, msg, &messageCount, errorCount, len(clients), sessionLogger)
			case err, ok := <-errorsCh:
				if !ok {
					return
				}
				handleError(ui, err, messageCount, &errorCount, len(clients), sessionLogger)
			}
		}
	}()
	return messageHandlerDone
}

func handleMessage(ui *UI, msg MonitorMessage, messageCount *int, errorCount, clientCount int, sessionLogger *SessionLogger) {
	ui.AddMessage(msg)
	*messageCount++
	ui.UpdateStatus(fmt.Sprintf("Messages: %d | Errors: %d | Connections: %d", *messageCount, errorCount, clientCount))

	if sessionLogger != nil {
		// Let zerolog handle the timestamp - just log the message content without color
		logMessage := fmt.Sprintf("[%s] %s: %s",
			msg.Source,
			msg.DisplayTopic,
			msg.Payload)
		if err := sessionLogger.Log(logMessage); err != nil {
			log.Error().Err(err).Msg("Failed to write to session log")
		}
	}
}

func handleError(ui *UI, err error, messageCount int, errorCount *int, clientCount int, sessionLogger *SessionLogger) {
	ui.AddError(err)
	if err != nil {
		*errorCount++
		ui.UpdateStatus(fmt.Sprintf("Messages: %d | Errors: %d | Connections: %d", messageCount, *errorCount, clientCount))

		if sessionLogger != nil {
			logMessage := fmt.Sprintf("Connection event: %s",
				err.Error())
			if logErr := sessionLogger.Log(logMessage); logErr != nil {
				log.Error().Err(logErr).Msg("Failed to write error to session log")
			}
		}
	}
}

func waitForShutdownSignal(sigCh chan os.Signal, uiDone chan error) string {
	select {
	case sig := <-sigCh:
		return fmt.Sprintf("Received signal: %v", sig)
	case err := <-uiDone:
		if err != nil {
			return fmt.Sprintf("UI error: %v", err)
		}
		return "UI exited normally"
	}
}

func performGracefulShutdown(cancel context.CancelFunc,
	ui *UI, clients []*MQTTClient, messageHandlerDone chan struct{},
	messagesCh chan MonitorMessage, errorsCh chan error, shutdownReason string) {
    
    // Don't log to console during shutdown - it interferes with TUI
    cancel()
    ui.Stop()

    disconnectClients(clients)
    waitForMessageHandler(messageHandlerDone)

    close(messagesCh)
    close(errorsCh)
}

func disconnectClients(clients []*MQTTClient) {
    // Remove console logging during disconnect
    disconnectDone := make(chan struct{})
    go func() {
        defer close(disconnectDone)
        var wg sync.WaitGroup
        for _, client := range clients {
            wg.Add(1)
            go func(c *MQTTClient) {
                defer wg.Done()
                c.Disconnect()
            }(client)
        }
        wg.Wait()
    }()

    select {
    case <-disconnectDone:
        // Silent completion
    case <-time.After(2 * time.Second):
        // Silent timeout
    }
}

func waitForMessageHandler(messageHandlerDone chan struct{}) {
    select {
    case <-messageHandlerDone:
        // Silent completion
    case <-time.After(1 * time.Second):
        // Silent timeout
    }
}