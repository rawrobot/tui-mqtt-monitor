package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/rawrobot/tui-mqtt-monitor/internal/mqtt"
)

type MQTTClient struct {
	config     ConnectionConfig
	client     *mqtt.Client
	messagesCh chan MonitorMessage
	errorsCh   chan error
	name       string
	ctx        context.Context
	topicDepth int
	logger     zerolog.Logger
	color      string
}

func NewMQTTClient(config ConnectionConfig, messagesCh chan MonitorMessage, errorsCh chan error, topicDepth int) *MQTTClient {
	logger := log.With().
		Str("component", "mqtt-client").
		Str("connection", config.Name).
		Logger()

	mqttConfig := config.ToMQTTConfig()
	client := mqtt.NewClient(mqttConfig, logger)

	return &MQTTClient{
		config:     config,
		client:     client,
		messagesCh: messagesCh,
		errorsCh:   errorsCh,
		name:       config.Name,
		topicDepth: topicDepth,
		logger:     logger,
	}
}

func (c *MQTTClient) SetContext(ctx context.Context) {
	c.ctx = ctx
}

// Add a method to set the color
func (c *MQTTClient) SetColor(color string) {
	c.color = color
}

func (c *MQTTClient) Connect() error {
	// Set up message handler
	c.client.SetMessageHandler(func(msg mqtt.Message) {
		message := NewMonitorMessage(msg, c.name, c.topicDepth, c.color)

		select {
		case c.messagesCh <- message:
		case <-c.ctx.Done():
			return
		default:
			// Channel is full, drop the message to prevent blocking
			c.logger.Warn().Msg("Message channel full, dropping message")
		}
	})

	// Set up connection handler
	c.client.SetConnectionHandler(func(connected bool, err error) {
		var statusErr error
		if connected {
			// Subscribe to topics after successful connection
			c.logger.Info().Msg("Connected successfully, subscribing to topics...")
			if subscribeErr := c.subscribeToTopics(); subscribeErr != nil {
				statusErr = fmt.Errorf("%s: subscription error: %w", c.name, subscribeErr)
			} else {
				statusErr = fmt.Errorf("%s: connected and subscribed successfully", c.name)
			}
		} else if err != nil {
			statusErr = fmt.Errorf("%s: connection error: %w", c.name, err)
		} else {
			statusErr = fmt.Errorf("%s: disconnected", c.name)
		}

		select {
		case c.errorsCh <- statusErr:
		case <-c.ctx.Done():
			return
		default:
			// Channel is full, drop the error to prevent blocking
		}
	})

	// Set QoS level
	c.client.SetQoS(c.config.QoS)

	// Connect to broker
	if err := c.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	return nil
}

// safeErrorSend safely sends error to error channel without blocking
func (m *MQTTClient) safeErrorSend(err error) {
	if m.ctx != nil {
		select {
		case m.errorsCh <- err:
			// Error sent successfully
		case <-m.ctx.Done():
			// Context cancelled, stop trying
			return
		default:
			// Error channel is full, ignore to prevent blocking
		}
	} else {
		select {
		case m.errorsCh <- err:
		default:
		}
	}
}

func (m *MQTTClient) Disconnect() {
	defer func() {
		if r := recover(); r != nil {
			m.safeErrorSend(fmt.Errorf("[%s] disconnect panic: %v", m.name, r))
		}
	}()

	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect()
	}
}

// subscribeToTopics subscribes to all configured topics
func (c *MQTTClient) subscribeToTopics() error {
	if len(c.config.Topics) == 0 {
		c.logger.Warn().Msg("No topics configured for subscription")
		return nil
	}

	c.logger.Info().
		Strs("topics", c.config.Topics).
		Uint8("qos", c.config.QoS).
		Msg("Subscribing to topics")

	// Subscribe to all configured topics
	if err := c.client.Subscribe(c.config.Topics...); err != nil {
		c.logger.Error().Err(err).Msg("Failed to subscribe to topics")
		return err
	}

	c.logger.Info().
		Strs("topics", c.config.Topics).
		Msg("Successfully subscribed to all topics")

	return nil
}
