package main

import (
	"time"

	"github.com/rawrobot/tui-mqtt-monitor/internal/mqtt"
)

type MonitorMessage struct {
	Topic        string
	DisplayTopic string
	Payload      string
	Source       string
	Timestamp    time.Time
	QoS          byte
	Retained     bool
	Color        string
}

// NewMonitorMessage creates a new Message from mqtt.Message
func NewMonitorMessage(mqttMsg mqtt.Message, source string, topicDepth int, color string) MonitorMessage {
	displayTopic := mqtt.TruncateTopic(mqttMsg.Topic, topicDepth)
	payload := mqtt.SanitizePayload(mqttMsg.Payload)

	return MonitorMessage{
		Topic:        mqttMsg.Topic,
		DisplayTopic: displayTopic,
		Payload:      payload,
		Source:       source,
		Timestamp:    mqttMsg.Timestamp,
		QoS:          mqttMsg.QoS,
		Retained:     mqttMsg.Retained,
		Color:        color,
	}
}
