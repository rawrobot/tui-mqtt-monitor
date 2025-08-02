.PHONY: build-monitor build-all run clean deps test lint version help

# Define variables
SOURCES := $(shell find . -type f -name '*.go' -not -path "./vendor/*")
mkfile_path := $(abspath $(lastword $(MAKEFILE_LIST)))
CURRENT_DIR := $(notdir $(dir $(mkfile_path)))
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
GIT_HASH := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ") 

GO_DEF_FLAGS := -ldflags "-s -w -extldflags=-static -X main.githash=$(GIT_HASH) -X main.buildDate=$(BUILD_DATE)"
GO_STATIC_FLAGS := -ldflags "-s -w -linkmode external -extldflags=-static -X main.githash=$(GIT_HASH) -X main.buildDate=$(BUILD_DATE)"
GO_COVERAGE_FLAGS := -cover

BIN_DIR := ./bin
CMD_DIR := ./cmd

MONITOR_NAME=tui-mqtt-monitor
MONITOR_CONFIG_FILE=config.toml

.DEFAULT_GOAL := build-monitor

build-monitor: 
	go build -o $(BIN_DIR)/$(MONITOR_NAME) $(GO_STATIC_FLAGS) $(CMD_DIR)/mqtt-monitor/...

build-all: build-monitor build-test-publisher


run: build-monitor
	$(BIN_DIR)/$(MONITOR_NAME) -config ./$(MONITOR_CONFIG_FILE)

clean:
	go clean
	rm -f $(BIN_DIR)/*

deps:
	go mod tidy
	go mod vendor

test:
	go test ./... $(GO_COVERAGE_FLAGS)

lint:
	golangci-lint run

version:
	@echo "Git Hash: $(GIT_HASH)"
	@echo "Build Date: $(BUILD_DATE)"

help:
	@echo "Available targets:"
	@echo "  build-monitor    : Build the MQTT monitor"
	@echo "  build-all        : Build all tools"
	@echo "  run              : Build and run the MQTT monitor"
	@echo "  clean            : Remove built binaries"
	@echo "  deps             : Ensure dependencies are up to date"
	@echo "  test             : Run tests"
	@echo "  lint             : Run linter"
	@echo "  version          : Display version information"
	
debug:
    @echo "Current Directory: $(CURRENT_DIR)"
	@echo "Makefile Path: $(mkfile_path)"
	@echo "Directory Path: $(dir $(mkfile_path))"
