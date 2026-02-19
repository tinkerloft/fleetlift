// Package main is the worker entry point.
package main

import (
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	temporalactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdkinterceptor "go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	"github.com/tinkerloft/fleetlift/internal/activity"
	internalclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/logging"
	"github.com/tinkerloft/fleetlift/internal/metrics"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/docker" // register docker provider
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/k8s"    // register k8s provider
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func main() {
	// Structured JSON logging
	sl := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(sl)
	temporalLogger := logging.NewSlogAdapter(sl)

	// Parse command-line flags
	debugNoCleanup := flag.Bool("debug-no-cleanup", false, "Skip container cleanup on failure (for debugging)")
	flag.Parse()

	// Set environment variable for activities to check
	if *debugNoCleanup {
		if err := os.Setenv("DEBUG_NO_CLEANUP", "true"); err != nil {
			log.Printf("Warning: failed to set DEBUG_NO_CLEANUP: %v", err)
		}
		log.Println("DEBUG MODE: Container cleanup disabled - containers will persist after workflow completion")
	}
	// Validate configuration at startup
	configMode := activity.ConfigModeWarn
	if os.Getenv("REQUIRE_CONFIG") == "true" {
		configMode = activity.ConfigModeRequire
	}
	if err := activity.CheckConfig(configMode); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Set up Prometheus metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New()
	if err := metrics.RegisterWith(reg, m); err != nil {
		log.Fatalf("Failed to register metrics: %v", err)
	}

	// Expose /metrics on a dedicated port
	metricsAddr := getEnvOrDefault("METRICS_ADDR", ":9090")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		log.Printf("Metrics server listening on %s/metrics", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Get Temporal address
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	// Connect to Temporal
	c, err := client.Dial(client.Options{
		HostPort: temporalAddr,
		Logger:   temporalLogger,
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	log.Printf("Connected to Temporal at %s", temporalAddr)
	log.Printf("Task queue: %s", internalclient.TaskQueue)

	// Create sandbox provider
	providerName := os.Getenv("SANDBOX_PROVIDER")
	cfg := sandbox.ProviderConfig{
		Namespace:      getEnvOrDefault("SANDBOX_NAMESPACE", "sandbox-isolated"),
		AgentImage:     getEnvOrDefault("AGENT_IMAGE", "fleetlift-agent:latest"),
		KubeconfigPath: os.Getenv("KUBECONFIG"),
	}
	provider, err := sandbox.NewProvider(providerName, cfg)
	if err != nil {
		log.Fatalf("Failed to create sandbox provider: %v", err)
	}
	log.Printf("Sandbox provider: %s", provider.Name())

	// Create activities
	sandboxActivities := activity.NewSandboxActivities(provider)
	claudeActivities := activity.NewClaudeCodeActivities(provider)
	deterministicActivities := activity.NewDeterministicActivities(provider)
	githubActivities := activity.NewGitHubActivities(provider)
	slackActivities := activity.NewSlackActivities()
	reportActivities := activity.NewReportActivities(provider)
	steeringActivities := activity.NewSteeringActivities(provider)
	agentActivities := activity.NewAgentActivities(provider)
	knowledgeActivities := activity.NewKnowledgeActivities()

	// Create worker
	w := worker.New(c, internalclient.TaskQueue, worker.Options{
		Interceptors: []sdkinterceptor.WorkerInterceptor{metrics.NewInterceptor(m)},
	})

	// Register workflows
	w.RegisterWorkflow(workflow.Transform)
	w.RegisterWorkflow(workflow.TransformGroup)
	w.RegisterWorkflow(workflow.TransformV2)

	// Register activities with explicit names to match workflow constants
	w.RegisterActivityWithOptions(sandboxActivities.ProvisionSandbox, temporalactivity.RegisterOptions{Name: activity.ActivityProvisionSandbox})
	w.RegisterActivityWithOptions(sandboxActivities.CloneRepositories, temporalactivity.RegisterOptions{Name: activity.ActivityCloneRepositories})
	w.RegisterActivityWithOptions(sandboxActivities.CleanupSandbox, temporalactivity.RegisterOptions{Name: activity.ActivityCleanupSandbox})
	w.RegisterActivityWithOptions(sandboxActivities.RunVerifiers, temporalactivity.RegisterOptions{Name: activity.ActivityRunVerifiers})
	w.RegisterActivityWithOptions(claudeActivities.RunClaudeCode, temporalactivity.RegisterOptions{Name: activity.ActivityRunClaudeCode})
	w.RegisterActivityWithOptions(deterministicActivities.ExecuteDeterministic, temporalactivity.RegisterOptions{Name: activity.ActivityExecuteDeterministic})
	w.RegisterActivityWithOptions(githubActivities.CreatePullRequest, temporalactivity.RegisterOptions{Name: activity.ActivityCreatePullRequest})
	w.RegisterActivityWithOptions(slackActivities.NotifySlack, temporalactivity.RegisterOptions{Name: activity.ActivityNotifySlack})
	w.RegisterActivityWithOptions(reportActivities.CollectReport, temporalactivity.RegisterOptions{Name: activity.ActivityCollectReport})
	w.RegisterActivityWithOptions(reportActivities.ValidateSchema, temporalactivity.RegisterOptions{Name: activity.ActivityValidateSchema})
	w.RegisterActivityWithOptions(steeringActivities.GetDiff, temporalactivity.RegisterOptions{Name: activity.ActivityGetDiff})
	w.RegisterActivityWithOptions(steeringActivities.GetVerifierOutput, temporalactivity.RegisterOptions{Name: activity.ActivityGetVerifierOutput})
	w.RegisterActivityWithOptions(sandboxActivities.ProvisionAgentSandbox, temporalactivity.RegisterOptions{Name: activity.ActivityProvisionAgentSandbox})
	w.RegisterActivityWithOptions(agentActivities.SubmitTaskManifest, temporalactivity.RegisterOptions{Name: activity.ActivitySubmitTaskManifest})
	w.RegisterActivityWithOptions(agentActivities.WaitForAgentPhase, temporalactivity.RegisterOptions{Name: activity.ActivityWaitForAgentPhase})
	w.RegisterActivityWithOptions(agentActivities.ReadAgentResult, temporalactivity.RegisterOptions{Name: activity.ActivityReadAgentResult})
	w.RegisterActivityWithOptions(agentActivities.SubmitSteeringAction, temporalactivity.RegisterOptions{Name: activity.ActivitySubmitSteeringAction})
	w.RegisterActivityWithOptions(knowledgeActivities.CaptureKnowledge, temporalactivity.RegisterOptions{Name: activity.ActivityCaptureKnowledge})
	w.RegisterActivityWithOptions(knowledgeActivities.EnrichPrompt, temporalactivity.RegisterOptions{Name: activity.ActivityEnrichPrompt})

	log.Println("Worker started. Press Ctrl+C to stop.")

	// Run worker - Temporal's InterruptCh handles graceful shutdown
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	log.Println("Worker stopped")
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
