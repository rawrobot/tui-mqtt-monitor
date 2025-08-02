package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rawrobot/tui-mqtt-monitor/internal/mqtt"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Logging     Logging            `toml:"logging"`
	Connections []ConnectionConfig `toml:"connection"`
	Display     DisplayConfig      `toml:"display"`
}

type Logging struct {
	Level                 string `toml:"level"`
	Pretty                bool   `toml:"pretty"`
	OutputDir             string `toml:"output_dir"`
	EnableSessionLog      bool   `toml:"enable_session_log"`
	SessionLogMaxDuration string `toml:"session_log_max_duration"`
}

type DisplayConfig struct {
	TopicDepth int `toml:"topic_depth"` // Number of topic levels to show from the end
}

type ConnectionConfig struct {
	Name                  string   `toml:"name"`
	Server                string   `toml:"server"`
	User                  string   `toml:"user,omitempty"`
	Password              string   `toml:"password,omitempty"`
	TLSCertFile           string   `toml:"tls_cert_file,omitempty"`
	TLSKeyFile            string   `toml:"tls_key_file,omitempty"`
	TLSCAFile             string   `toml:"tls_ca_file,omitempty"`
	TLSInsecureSkipVerify bool     `toml:"tls_insecure_skip_verify,omitempty"`
	Topics                []string `toml:"topics"` // Array of topics
	ClientIDBase          string   `toml:"client_id_base"`
	QoS                   byte     `toml:"qos,omitempty"` // QoS level (0, 1, or 2)
}

func LoadConfig(filename string) (*Config, error) {
	var config Config

	// Set defaults
	config.Display.TopicDepth = 3 // Default to showing last 3 levels

	_, err := toml.DecodeFile(filename, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Override with environment variables if available
	for i := range config.Connections {
		conn := &config.Connections[i]

		// Override credentials from environment variables
		if envUser := os.Getenv(fmt.Sprintf("MQTT_USER_%d", i)); envUser != "" {
			conn.User = envUser
		}
		if envPass := os.Getenv(fmt.Sprintf("MQTT_PASSWORD_%d", i)); envPass != "" {
			conn.Password = envPass
		}

		// Global environment variables (for single connection setups)
		if len(config.Connections) == 1 {
			if envUser := os.Getenv("MQTT_USER"); envUser != "" {
				conn.User = envUser
			}
			if envPass := os.Getenv("MQTT_PASSWORD"); envPass != "" {
				conn.Password = envPass
			}
		}
	}

	// Validate required fields
	for i, conn := range config.Connections {
		if conn.Name == "" {
			config.Connections[i].Name = fmt.Sprintf("Connection-%d", i+1)
		}
		if conn.Server == "" {
			return nil, fmt.Errorf("server is required for connection %s", conn.Name)
		}
		if len(conn.Topics) == 0 {
			return nil, fmt.Errorf("at least one topic is required for connection %s", conn.Name)
		}
		if conn.ClientIDBase == "" {
			config.Connections[i].ClientIDBase = fmt.Sprintf("mqtt-monitor-%s", conn.Name)
		}

		// Set default QoS if not specified
		if conn.QoS > 2 {
			config.Connections[i].QoS = 1 // Default to QoS 1
		}

		// Validate TLS configuration
		if err := validateTLSConfig(&config.Connections[i]); err != nil {
			return nil, fmt.Errorf("TLS validation failed for connection %s: %w", conn.Name, err)
		}
	}

	// Validate display configuration
	if config.Display.TopicDepth < 1 {
		config.Display.TopicDepth = 3 // Default fallback
	}

	return &config, nil
}

func validateTLSConfig(conn *ConnectionConfig) error {
	// Check if TLS is required based on server URL
	isTLS := strings.HasPrefix(conn.Server, "ssl://") ||
		strings.HasPrefix(conn.Server, "tls://") ||
		strings.HasPrefix(conn.Server, "mqtts://")

	// If client certificate is specified, both cert and key must be present
	if (conn.TLSCertFile != "" && conn.TLSKeyFile == "") ||
		(conn.TLSCertFile == "" && conn.TLSKeyFile != "") {
		return fmt.Errorf("both tls_cert_file and tls_key_file must be specified together")
	}

	// Validate certificate files exist if specified
	if conn.TLSCertFile != "" {
		if _, err := os.Stat(conn.TLSCertFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS certificate file not found: %s", conn.TLSCertFile)
		}
	}

	if conn.TLSKeyFile != "" {
		if _, err := os.Stat(conn.TLSKeyFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS key file not found: %s", conn.TLSKeyFile)
		}
	}

	if conn.TLSCAFile != "" {
		if _, err := os.Stat(conn.TLSCAFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS CA file not found: %s", conn.TLSCAFile)
		}
	}

	// Warn about insecure configurations
	if isTLS && conn.TLSInsecureSkipVerify {
		fmt.Fprintf(os.Stderr, "WARNING: TLS certificate verification disabled for %s - this is insecure!\n", conn.Name)
	}

	return nil
}

// ToMQTTConfig converts ConnectionConfig to mqtt.Config
func (c *ConnectionConfig) ToMQTTConfig() mqtt.Config {
	return mqtt.Config{
		BrokerURL:             c.Server,
		ClientID:              c.GetUniqueClientID(),
		Username:              c.User,
		Password:              c.Password,
		CleanSession:          true,
		ConnectRetryInterval:  5 * time.Second,
		MaxReconnectInterval:  60 * time.Second,
		TLSCertFile:           c.TLSCertFile,
		TLSKeyFile:            c.TLSKeyFile,
		TLSCAFile:             c.TLSCAFile,
		TLSInsecureSkipVerify: c.TLSInsecureSkipVerify,
	}
}

func (c *ConnectionConfig) GetUniqueClientID() string {
	return fmt.Sprintf("%s-%d", c.ClientIDBase, time.Now().Unix())
}

func (c *ConnectionConfig) GetTLSConfig() (*tls.Config, error) {
	if !c.needsTLS() {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.TLSInsecureSkipVerify,
	}

	// Load client certificate if provided
	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if c.TLSCAFile != "" {
		caCert, err := os.ReadFile(c.TLSCAFile)
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

func (c *ConnectionConfig) needsTLS() bool {
	return strings.HasPrefix(c.Server, "ssl://") ||
		strings.HasPrefix(c.Server, "tls://") ||
		strings.HasPrefix(c.Server, "mqtts://") ||
		c.TLSCertFile != "" ||
		c.TLSCAFile != "" ||
		c.TLSInsecureSkipVerify
}

// FormatTopicForDisplay formats topic according to configured depth
func FormatTopicForDisplay(topic string, depth int) string {
	if depth <= 0 {
		return topic
	}

	parts := strings.Split(topic, "/")
	if len(parts) <= depth {
		return topic
	}

	// Take the last 'depth' parts
	return strings.Join(parts[len(parts)-depth:], "/")
}
