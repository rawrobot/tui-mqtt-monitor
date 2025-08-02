package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type SensorData struct {
	Temperature float64   `json:"temperature"`
	Humidity    float64   `json:"humidity"`
	Timestamp   time.Time `json:"timestamp"`
	SensorID    string    `json:"sensor_id"`
}

func main() {
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	topic := flag.String("topic", "sensors/test/data", "MQTT topic to publish to")
	interval := flag.Duration("interval", 2*time.Second, "Publishing interval")
	count := flag.Int("count", 0, "Number of messages to send (0 for infinite)")
	flag.Parse()

	opts := mqtt.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID("mqtt-test-publisher")

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect: %v", token.Error())
	}
	defer client.Disconnect(250)

	fmt.Printf("Publishing to %s on topic %s every %v\n", *broker, *topic, *interval)
	fmt.Println("Press Ctrl+C to stop")

	sent := 0
	for {
		if *count > 0 && sent >= *count {
			break
		}

		// Generate random sensor data
		data := SensorData{
			Temperature: 20.0 + rand.Float64()*15.0, // 20-35°C
			Humidity:    30.0 + rand.Float64()*40.0, // 30-70%
			Timestamp:   time.Now(),
			SensorID:    fmt.Sprintf("sensor_%02d", rand.Intn(10)),
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			log.Printf("Failed to marshal JSON: %v", err)
			continue
		}

		token := client.Publish(*topic, 0, false, jsonData)
		if token.Wait() && token.Error() != nil {
			log.Printf("Failed to publish: %v", token.Error())
		} else {
			sent++
			fmt.Printf("Sent message %d: %s\n", sent, string(jsonData))
		}

		time.Sleep(*interval)
	}

	fmt.Printf("Published %d messages\n", sent)
}
