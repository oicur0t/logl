.PHONY: all build build-tailer build-server test clean docker-build docker-push run-local stop-local certs lint help

# Build variables
BINARY_DIR=bin
TAILER_BINARY=$(BINARY_DIR)/logl-tailer
SERVER_BINARY=$(BINARY_DIR)/logl-server

# Docker/Podman settings
CONTAINER_TOOL?=podman
IMAGE_PREFIX?=logl
IMAGE_TAG?=latest

all: build

## build: Build both tailer and server binaries
build: build-tailer build-server

## build-tailer: Build the tailer binary
build-tailer:
	@echo "Building logl-tailer..."
	@mkdir -p $(BINARY_DIR)
	go build -o $(TAILER_BINARY) ./cmd/logl-tailer

## build-server: Build the server binary
build-server:
	@echo "Building logl-server..."
	@mkdir -p $(BINARY_DIR)
	go build -o $(SERVER_BINARY) ./cmd/logl-server

## test: Run tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

## lint: Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	golangci-lint run

## certs: Generate mTLS certificates
certs:
	@echo "Generating certificates..."
	@chmod +x deployments/certs/generate-certs.sh
	@cd deployments/certs && ./generate-certs.sh

## docker-build: Build Docker/Podman images
docker-build:
	@echo "Building container images with $(CONTAINER_TOOL)..."
	$(CONTAINER_TOOL) build -f deployments/podman/Dockerfile.tailer -t $(IMAGE_PREFIX)-tailer:$(IMAGE_TAG) .
	$(CONTAINER_TOOL) build -f deployments/podman/Dockerfile.server -t $(IMAGE_PREFIX)-server:$(IMAGE_TAG) .

## docker-push: Push Docker/Podman images to registry
docker-push:
	@echo "Pushing container images..."
	$(CONTAINER_TOOL) push $(IMAGE_PREFIX)-tailer:$(IMAGE_TAG)
	$(CONTAINER_TOOL) push $(IMAGE_PREFIX)-server:$(IMAGE_TAG)

## run-local: Run locally with podman-compose
run-local: certs
	@echo "Starting services with podman-compose..."
	@cd deployments/podman && podman-compose up -d

## stop-local: Stop local environment
stop-local:
	@echo "Stopping services..."
	@cd deployments/podman && podman-compose down

## clean: Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BINARY_DIR)
	go clean

## init: Initialize Go modules
init:
	@echo "Initializing Go modules..."
	go mod tidy
	go mod download

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
