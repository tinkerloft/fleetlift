// Package main is the sandbox sidecar agent entrypoint.
//
// Usage: fleetlift-agent serve
//
// The agent watches for a task manifest at /workspace/.fleetlift/manifest.json,
// executes the full pipeline (clone → transform → verify → collect),
// and writes structured results to /workspace/.fleetlift/result.json.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintf(os.Stderr, "Usage: fleetlift-agent serve\n")
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	basePath := protocol.BasePath

	// Allow override for testing
	if envPath := os.Getenv("FLEETLIFT_BASE_PATH"); envPath != "" {
		basePath = envPath
	}

	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		logger.Error("Failed to create base directory", "path", basePath, "error", err)
		os.Exit(1)
	}

	pipeline := agent.NewDefaultPipeline(basePath)

	logger.Info("Sidecar agent starting", "basePath", basePath)

	if err := pipeline.Run(context.Background()); err != nil {
		logger.Error("Pipeline failed", "error", err)
		os.Exit(1)
	}
}
