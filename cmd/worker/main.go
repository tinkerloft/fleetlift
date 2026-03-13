package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/agent"
	"github.com/tinkerloft/fleetlift/internal/db"
	"github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Validate configuration
	if err := activity.CheckConfig(activity.ConfigModeWarn); err != nil {
		log.Fatal(err)
	}

	// Connect to Temporal
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}
	c, err := client.Dial(client.Options{HostPort: temporalAddr})
	if err != nil {
		log.Fatalf("temporal connect: %v", err)
	}
	defer c.Close()

	// Connect to database
	database, err := db.Connect(context.Background())
	if err != nil {
		log.Fatalf("database connect: %v", err)
	}

	// Create sandbox client
	sbClient := opensandbox.New(
		os.Getenv("OPENSANDBOX_DOMAIN"),
		os.Getenv("OPENSANDBOX_API_KEY"),
	)

	// Create credential store (optional — only if encryption key is set)
	var credStore activity.CredentialStore
	if encKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY"); encKey != "" {
		cs, err := activity.NewDBCredentialStore(database, encKey)
		if err != nil {
			log.Fatalf("credential store: %v", err)
		}
		credStore = cs
	}

	// Create activities struct with all dependencies
	acts := &activity.Activities{
		Sandbox:   sbClient,
		DB:        database,
		CredStore: credStore,
		AgentRunners: map[string]agent.Runner{
			"claude-code": agent.NewClaudeCodeRunner(sbClient),
		},
	}

	// Create and configure worker
	taskQueue := os.Getenv("TEMPORAL_TASK_QUEUE")
	if taskQueue == "" {
		taskQueue = "fleetlift"
	}
	w := worker.New(c, taskQueue, worker.Options{})

	// Register workflows
	w.RegisterWorkflow(workflow.DAGWorkflow)
	w.RegisterWorkflow(workflow.StepWorkflow)

	// Register activities
	w.RegisterActivity(acts)

	// Register standalone activity structs (Slack, GitHub)
	w.RegisterActivity(activity.NewSlackActivities())
	w.RegisterActivity(activity.NewGitHubActivities())

	slog.Info("starting fleetlift worker", "task_queue", taskQueue, "temporal", temporalAddr)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("worker run: %v", err)
	}
}
