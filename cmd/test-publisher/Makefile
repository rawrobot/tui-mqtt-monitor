.PHONY: build run clean

BINARY_NAME=mqtt-test-publisher

build:
	go build -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME)

clean:
	go clean
	rm -f $(BINARY_NAME)

test-local: build
	./$(BINARY_NAME) -broker tcp://localhost:1883 -topic sensors/test/data -interval 1s

test-public: build
	./$(BINARY_NAME) -broker tcp://test.mosquitto.org:1883 -topic test/mqtt-monitor/data -interval 2s -count 10

.DEFAULT_GOAL := build