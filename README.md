# MQTT Monitor

A Go-based console application designed to monitor MQTT messages from multiple brokers simultaneously with enhanced visualization and logging capabilities.

## Features

### Core Functionality
- Monitor multiple MQTT brokers simultaneously
- TOML configuration file support
- Full TLS/SSL support with various certificate options
- Real-time message display with timestamps
- Connection status and error monitoring
- Unique client IDs for each connection
- Safe message handling

### Enhanced UI Features
- **Color-coded sources**: Each MQTT broker connection displays messages in a different color for easy identification
- **Scrollable console interface** with message buffering (10,000 messages)
- **Multi-panel layout**:
  - Main messages view (top, 3/4 of screen)
  - Connection status & errors view (middle, 1/4 of screen)  
  - Status bar (bottom, fixed height)
- **Interactive controls**:
  - `Ctrl+C` or `Esc` to quit
  - `Tab` to switch focus between message and error views
- **Dynamic topic display** with configurable depth truncation

### Session Logging
- **Session log files**: Automatically save all messages to timestamped log files
- **Configurable log duration**: Set maximum session duration (e.g., "1h", "30m")
- **Structured log format**: Includes timestamps, source identification, and full message content
- **Optional logging**: Can be enabled/disabled via configuration

### Multi-Broker Support
- **Named connections**: Each broker connection has a descriptive name
- **Independent configuration**: Each connection can have different:
  - Server URLs and ports
  - Authentication credentials
  - TLS settings
  - Topic subscriptions
  - QoS levels
- **Color assignment**: Automatic color assignment to distinguish between different brokers

## Demo

![Demo](./demo.gif)




## Configuration

### Basic Configuration Structure
```toml
# MQTT Monitor Configuration

[logging]
level = "info"                    # Log level: debug, info, warn, error
pretty = true                     # Pretty print logs
output_dir = "./data"             # Directory for session logs
enable_session_log = true         # Enable session logging
session_log_max_duration = "1h"   # Maximum session duration

[display]
topic_depth = 3                   # Number of topic levels to display

# Multiple broker connections
[[connection]]
name = "Production Broker"
server = "tcp://prod-mqtt.example.com:1883"
user = "prod_user"
password = "prod_password"
topics = ["sensors/+/data", "alerts/#"]
client_id_base = "monitor-prod"
qos = 1

[[connection]]
name = "Development Broker"  
server = "tcp://dev-mqtt.example.com:1883"
user = "dev_user"
password = "dev_password"
topics = ["test/+/data"]
client_id_base = "monitor-dev"
qos = 0
```

### Configuration Parameters

#### Display Configuration
- `topic_depth`: Number of topic levels to show from the end (default: 3)

#### Connection Configuration
- `name`: Human-readable name for the connection
- `server`: MQTT broker URL
  - `tcp://host:port` for plain connections
  - `ssl://host:port`, `tls://host:port`, or `mqtts://host:port` for TLS/SSL connections
- `user`: Username for authentication (optional)
- `password`: Password for authentication (optional)
- `tls_cert_file`: Path to client certificate file for mutual TLS (optional)
- `tls_key_file`: Path to client private key file for mutual TLS (optional)
- `tls_ca_file`: Path to custom CA certificate file (optional)
- `tls_insecure_skip_verify`: Skip TLS certificate verification (default: false)
- `topics`: Array of MQTT topic patterns to subscribe to (supports wildcards + and #)
- `qos`: Quality of Service level (0, 1, or 2, default: 1)
- `client_id_base`: Base name for generating unique client IDs

### TLS Configuration Examples

#### 1. Self-Signed Certificates
```toml
[[connection]]
name = "Self-Signed Broker"
server = "ssl://mqtt.local:8883"
tls_insecure_skip_verify = true
topics = ["sensors/#"]
```

#### 2. Custom CA Certificate
```toml
[[connection]]
name = "Custom CA Broker"
server = "ssl://mqtt.company.com:8883"
tls_ca_file = "/etc/ssl/certs/company-ca.pem"
topics = ["company/devices/+", "company/alerts/#"]
```

#### 3. Mutual TLS Authentication
```toml
[[connection]]
name = "Mutual TLS Broker"
server = "ssl://secure.example.com:8883"
tls_cert_file = "/etc/ssl/certs/client.pem"
tls_key_file = "/etc/ssl/private/client-key.pem"
tls_ca_file = "/etc/ssl/certs/ca.pem"
topics = ["secure/data/#"]
```

#### 4. System CA Certificates (Strict)
```toml
[[connection]]
name = "Public Broker"
server = "ssl://mqtt.cloudprovider.com:8883"
topics = ["cloud/data/#"]
# No TLS configuration = use system CAs with strict verification
```

### Environment Variable Support

Credentials can be overridden using environment variables:

- For single connection: `MQTT_USER` and `MQTT_PASSWORD`
- For multiple connections: `MQTT_USER_0`, `MQTT_PASSWORD_0`, `MQTT_USER_1`, `MQTT_PASSWORD_1`, etc.

Example:
```bash
export MQTT_USER="myusername"
export MQTT_PASSWORD="mypassword"
./mqtt-monitor
```

## Usage

```bash
# Run with default config file (config.toml)
./mqtt-monitor

# Run with custom config file
./mqtt-monitor -config /path/to/your/config.toml
```

### Keyboard Controls

- `Ctrl+C` or `Esc`: Quit the application
- `Tab`: Switch focus between message view and error/status view
- `Arrow keys` / `Page Up/Down`: Scroll through messages when focused

## Output Format

Messages are displayed in the following format:
```
[timestamp] [connection-name] [topic] [json-message]
```

Example:
```
2024-01-15 14:30:25.123 [Local Broker] temperature/data {"temp": 23.5, "humidity": 45.2}
```

**Note**: 
- All messages are automatically converted to single lines with tabs replaced by spaces for consistent display
- Topic display is truncated to the configured `topic_depth` levels (default: 3)

## Security Notes

- **Self-signed certificates**: Set `tls_insecure_skip_verify = true` to accept self-signed certificates
- **Custom CA certificates**: Provide the path to your CA certificate file for proper verification
- **Mutual TLS**: Use both `tls_cert_file` and `tls_key_file` for client certificate authentication
- **Credentials**: Store sensitive credentials securely and consider using environment variables for production deployments
- **File permissions**: Ensure certificate and key files have appropriate permissions (600 for private keys)

## Building and Installing

```bash
# Build the binary
make build

# Run directly
make run

# Install to /usr/local/bin (requires sudo)
make install

# Clean build artifacts
make clean
```

## Troubleshooting

### Connection Issues

- Check that the MQTT broker is accessible
- Verify credentials if authentication is required
- For TLS connections, ensure the server URL uses `ssl://`, `tls://`, or `mqtts://` prefix

### TLS Certificate Issues

- **Self-signed certificates**: Set `tls_insecure_skip_verify = true`
- **Custom CA**: Ensure the certificate file path is correct and the file contains valid PEM data
- **Mutual TLS**: Verify both certificate and key files are present and valid
- **Certificate verification errors**: Check that the certificate matches the server hostname
- **File permissions**: Ensure certificate files are readable and key files have secure permissions

### Configuration Issues

- **Missing topics**: At least one topic must be specified for each connection
- **Invalid QoS**: QoS values must be 0, 1, or 2 (defaults to 1 if invalid)
- **TLS file validation**: Certificate and key files are validated at startup

### Performance

- The tool buffers messages with configurable limits
- Message handler includes timeout protection to prevent blocking
- If you experience high message volumes, consider using more specific topic filters
- Adjust `topic_depth` to control topic display length

## License

MIT License