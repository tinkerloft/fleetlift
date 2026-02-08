.PHONY: build test clean fleetlift-worker fleetlift fleetlift-agent all temporal-dev temporal-up temporal-down temporal-logs sandbox-build

# Build all binaries
all: build

# Build binaries
build:
	go build -o bin/fleetlift-worker ./cmd/worker
	go build -o bin/fleetlift ./cmd/cli
	CGO_ENABLED=0 go build -o bin/fleetlift-agent ./cmd/agent

# Build worker only
fleetlift-worker:
	go build -o bin/fleetlift-worker ./cmd/worker

# Build CLI only
fleetlift:
	go build -o bin/fleetlift ./cmd/cli

# Build agent binary (statically compiled for sandbox image)
fleetlift-agent:
	CGO_ENABLED=0 go build -o bin/fleetlift-agent ./cmd/agent

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Run the worker (requires Temporal server)
run-worker: fleetlift-worker
	./bin/fleetlift-worker

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Start Temporal dev server (lightweight, in-memory)
temporal-dev:
	temporal server start-dev --ui-port 8233

# Start Temporal with docker-compose (persistent, production-like)
temporal-up:
	docker compose up -d

# Stop Temporal docker-compose
temporal-down:
	docker compose down

# View Temporal logs
temporal-logs:
	docker compose logs -f temporal

# Build sandbox image (copies agent binary into build context)
sandbox-build: fleetlift-agent
	cp bin/fleetlift-agent docker/fleetlift-agent
	docker build -f docker/Dockerfile.sandbox -t claude-code-sandbox:latest docker/
	rm -f docker/fleetlift-agent
