package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// Config represents MQTT client configuration
type Config struct {
	BrokerURL             string        `toml:"broker_url"`
	ClientID              string        `toml:"client_id"`
	Username              string        `toml:"username,omitempty"`
	Password              string        `toml:"password,omitempty"`
	CleanSession          bool          `toml:"clean_session"`
	ConnectRetryInterval  time.Duration `toml:"connect_retry_interval"`
	MaxReconnectInterval  time.Duration `toml:"max_reconnect_interval"`
	TLSCertFile           string        `toml:"tls_cert_file,omitempty"`
	TLSKeyFile            string        `toml:"tls_key_file,omitempty"`
	TLSCAFile             string        `toml:"tls_ca_file,omitempty"`
	TLSInsecureSkipVerify bool          `toml:"tls_insecure_skip_verify,omitempty"`
}

// Message represents an MQTT message
type Message struct {
	Topic     string
	Payload   []byte
	QoS       byte
	Retained  bool
	Timestamp time.Time
}

// MessageHandler is a function type for handling received messages
type MessageHandler func(msg Message)

// ConnectionHandler is a function type for handling connection events
type ConnectionHandler func(connected bool, err error)

// Client represents a universal MQTT client
type Client struct {
	config            Config
	client            mqtt.Client
	logger            zerolog.Logger
	ctx               context.Context
	cancel            context.CancelFunc
	messageHandler    MessageHandler
	connectionHandler ConnectionHandler
	topics            []string
	qos               byte
}

// NewClient creates a new universal MQTT client
func NewClient(config Config, logger zerolog.Logger) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		config: config,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		qos:    1, // Default QoS
	}
}

// SetMessageHandler sets the message handler function
func (c *Client) SetMessageHandler(handler MessageHandler) {
	c.messageHandler = handler
}

// SetConnectionHandler sets the connection handler function
func (c *Client) SetConnectionHandler(handler ConnectionHandler) {
	c.connectionHandler = handler
}

// SetQoS sets the Quality of Service level for subscriptions
func (c *Client) SetQoS(qos byte) {
	c.qos = qos
}

// Connect establishes connection to the MQTT broker
func (c *Client) Connect() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(c.config.BrokerURL)
	opts.SetClientID(c.config.ClientID)
	opts.SetCleanSession(c.config.CleanSession)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)

	if c.config.ConnectRetryInterval > 0 {
		opts.SetConnectRetryInterval(c.config.ConnectRetryInterval)
	} else {
		opts.SetConnectRetryInterval(5 * time.Second)
	}

	if c.config.MaxReconnectInterval > 0 {
		opts.SetMaxReconnectInterval(c.config.MaxReconnectInterval)
	} else {
		opts.SetMaxReconnectInterval(60 * time.Second)
	}

	// Set credentials if provided
	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
		if c.config.Password != "" {
			opts.SetPassword(c.config.Password)
		}
	}

	// Configure TLS if needed
	if c.needsTLS() {
		tlsConfig, err := c.getTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to create TLS config: %w", err)
		}
		if tlsConfig != nil {
			opts.SetTLSConfig(tlsConfig)
		}
	}

	// Set connection handlers
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		c.logger.Warn().Err(err).Msg("MQTT connection lost")
		if c.connectionHandler != nil {
			c.connectionHandler(false, err)
		}
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, opts *mqtt.ClientOptions) {
		c.logger.Info().Msg("MQTT reconnecting")
		if c.connectionHandler != nil {
			c.connectionHandler(false, fmt.Errorf("reconnecting"))
		}
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		c.logger.Info().Msg("MQTT connected")
		if c.connectionHandler != nil {
			c.connectionHandler(true, nil)
		}

		// Re-subscribe to all topics on reconnect
		for _, topic := range c.topics {
			if err := c.subscribeToTopic(topic); err != nil {
				c.logger.Error().Err(err).Str("topic", topic).Msg("Failed to re-subscribe")
			}
		}
	})

	c.client = mqtt.NewClient(opts)

	c.logger.Info().
		Str("broker", c.config.BrokerURL).
		Str("client_id", c.config.ClientID).
		Msg("Connecting to MQTT broker")

	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

// Subscribe subscribes to one or more topics
func (c *Client) Subscribe(topics ...string) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	for _, topic := range topics {
		if err := c.subscribeToTopic(topic); err != nil {
			return err
		}
		c.topics = append(c.topics, topic)
	}

	return nil
}

// subscribeToTopic subscribes to a single topic
func (c *Client) subscribeToTopic(topic string) error {
	c.logger.Info().Str("topic", topic).Uint8("qos", c.qos).Msg("Subscribing to topic")

	token := c.client.Subscribe(topic, c.qos, c.internalMessageHandler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, token.Error())
	}

	c.logger.Info().Str("topic", topic).Msg("Successfully subscribed to topic")
	return nil
}

// internalMessageHandler handles incoming MQTT messages
func (c *Client) internalMessageHandler(client mqtt.Client, msg mqtt.Message) {
	message := Message{
		Topic:     msg.Topic(),
		Payload:   msg.Payload(),
		QoS:       msg.Qos(),
		Retained:  msg.Retained(),
		Timestamp: time.Now(),
	}

	if c.messageHandler != nil {
		c.messageHandler(message)
	}
}

// Publish publishes a message to a topic
func (c *Client) Publish(topic string, payload []byte, qos byte, retained bool) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	token := c.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", topic, token.Error())
	}

	return nil
}

// IsConnected returns true if the client is connected
func (c *Client) IsConnected() bool {
	return c.client != nil && c.client.IsConnected()
}

// Disconnect disconnects from the MQTT broker
func (c *Client) Disconnect() {
	if c.client != nil && c.client.IsConnected() {
		c.logger.Info().Msg("Disconnecting from MQTT broker")
		c.client.Disconnect(250)
	}
	c.cancel()
}

// Context returns the client's context
func (c *Client) Context() context.Context {
	return c.ctx
}

// needsTLS checks if TLS configuration is needed
func (c *Client) needsTLS() bool {
	return strings.HasPrefix(c.config.BrokerURL, "ssl://") ||
		strings.HasPrefix(c.config.BrokerURL, "tls://") ||
		strings.HasPrefix(c.config.BrokerURL, "mqtts://") ||
		c.config.TLSCertFile != "" ||
		c.config.TLSCAFile != "" ||
		c.config.TLSInsecureSkipVerify
}

// getTLSConfig creates TLS configuration
func (c *Client) getTLSConfig() (*tls.Config, error) {
	if !c.needsTLS() {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.config.TLSInsecureSkipVerify,
	}

	// Load client certificate if provided
	if c.config.TLSCertFile != "" && c.config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.config.TLSCertFile, c.config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if c.config.TLSCAFile != "" {
		caCert, err := os.ReadFile(c.config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// SanitizePayload sanitizes message payload for safe display without HTML escaping
func SanitizePayload(payload []byte) string {
	content := string(payload)

	// Limit message size to prevent memory issues
	const maxMessageSize = 512 // Increased from 128 to allow longer messages
	if len(content) > maxMessageSize {
		content = content[:maxMessageSize] + "..."
	}

	// Replace all tabs with spaces
	sanitized := strings.ReplaceAll(content, "\t", " ")

	// Replace all newlines and carriage returns with spaces
	sanitized = strings.ReplaceAll(sanitized, "\n", " ")
	sanitized = strings.ReplaceAll(sanitized, "\r", " ")

	// Replace any other control characters with spaces (but preserve printable characters)
	sanitized = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != ' ' {
			return ' '
		}
		return r
	}, sanitized)

	// Collapse multiple consecutive spaces into single space
	sanitized = strings.Join(strings.Fields(sanitized), " ")

	return sanitized
}