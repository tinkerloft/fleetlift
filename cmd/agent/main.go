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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintf(os.Stderr, "Usage: fleetlift-agent serve\n")
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	basePath := fleetproto.DefaultBasePath

	// Allow override for testing
	if envPath := os.Getenv("FLEETLIFT_BASE_PATH"); envPath != "" {
		basePath = envPath
	}

	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		logger.Error("Failed to create base directory", "path", basePath, "error", err)
		os.Exit(1)
	}

	proto := agent.NewProtocol(basePath, agent.OSFileSystem{})

	logger.Info("Sidecar agent starting", "basePath", basePath)

	if err := run(context.Background(), proto, basePath, logger); err != nil {
		logger.Error("Pipeline failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, proto *agent.Protocol, basePath string, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle SIGTERM/SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			logger.Info("Received shutdown signal")
			cancel()
		case <-ctx.Done():
		}
	}()

	logger.Info("Agent starting, waiting for manifest...")
	if err := proto.WriteStatus(fleetproto.AgentStatus{
		Phase:     fleetproto.PhaseInitializing,
		Message:   "Waiting for manifest",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		logger.Warn("Failed to write initial status", "error", err)
	}

	// Wait for manifest
	rawManifest, err := proto.WaitForManifest(ctx)
	if err != nil {
		return fmt.Errorf("waiting for manifest: %w", err)
	}

	var manifest fleetproto.TaskManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	logger.Info("Manifest received", "taskID", manifest.TaskID, "mode", manifest.Mode)

	// Validate manifest
	if err := agent.ValidateManifest(&manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	// Set timeout from manifest
	if manifest.TimeoutSeconds > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, time.Duration(manifest.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
	}

	// Execute the pipeline steps
	pipeline := agent.NewDefaultPipeline(basePath)
	return pipeline.Execute(ctx, &manifest)
}
