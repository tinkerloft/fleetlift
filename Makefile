.PHONY: build test clean fleetlift-worker fleetlift mcp-sidecar all temporal-dev temporal-up temporal-down temporal-logs sandbox-build agent-image kind-setup test-integration-k8s build-web dev-web opensandbox-up opensandbox-down opensandbox-logs init-local

# Build all binaries
all: build

# Build frontend
build-web:
	cd web && npm install && npm run build

# Run Vite dev server (proxies /api to :8080)
dev-web:
	cd web && npm run dev

# Build binaries
build: build-web
	go build -o bin/fleetlift-worker ./cmd/worker
	go build -o bin/fleetlift ./cmd/cli
	go build -o bin/fleetlift-server ./cmd/server
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar

# Build worker only
fleetlift-worker:
	go build -o bin/fleetlift-worker ./cmd/worker

# Build CLI only
fleetlift:
	go build -o bin/fleetlift ./cmd/cli

# Build MCP sidecar (for sandbox upload)
mcp-sidecar:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/fleetlift-mcp ./cmd/mcp-sidecar

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
	golangci-lint run --timeout=5m

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

# Build sandbox image
sandbox-build:
	docker build -f docker/Dockerfile.sandbox -t claude-code-sandbox:latest docker/

# Build agent init container image (minimal, FROM scratch)
agent-image:
	docker build -f docker/Dockerfile.agent -t fleetlift-agent:latest .

# Set up kind cluster with K8s manifests and agent image
kind-setup:
	./scripts/kind-setup.sh

# Run K8s integration tests (requires kind cluster)
test-integration-k8s:
	go test -tags=integration -v ./internal/sandbox/k8s/...

# Start OpenSandbox lifecycle server (pulls opensandbox/server:latest from Docker Hub)
# Worker env: OPEN_SANDBOX_DOMAIN=http://localhost:8090 OPEN_SANDBOX_USE_SERVER_PROXY=true
opensandbox-up:
	docker compose -f docker-compose.opensandbox.yaml up -d

# Stop OpenSandbox lifecycle server
opensandbox-down:
	docker compose -f docker-compose.opensandbox.yaml down

# View OpenSandbox lifecycle server logs
opensandbox-logs:
	docker compose -f docker-compose.opensandbox.yaml logs -f opensandbox-server

# Run local setup wizard (builds agent image and CLI first, then runs wizard)
init-local: sandbox-build fleetlift
	bin/fleetlift init-local
