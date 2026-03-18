package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/db"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/server"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
	"github.com/tinkerloft/fleetlift/internal/server/notify"
	"github.com/tinkerloft/fleetlift/internal/template"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Database
	database, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	// Temporal client
	temporalClient, err := client.Dial(client.Options{
		HostPort: envOr("TEMPORAL_ADDRESS", "localhost:7233"),
	})
	if err != nil {
		log.Fatalf("connect temporal: %v", err)
	}
	defer temporalClient.Close()

	// Auth provider
	ghProvider := auth.NewGitHubProvider(
		os.Getenv("GITHUB_CLIENT_ID"),
		os.Getenv("GITHUB_CLIENT_SECRET"),
		os.Getenv("GITHUB_CALLBACK_URL"),
	)

	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		log.Fatal("JWT_SECRET is required")
	}

	// Template registry
	builtinProvider, err := template.NewBuiltinProvider()
	if err != nil {
		log.Fatalf("load builtin templates: %v", err)
	}
	dbProvider := template.NewDBProvider(database)
	registry := template.NewRegistry(builtinProvider, dbProvider)

	// Credentials handler
	encKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	credHandler, err := handlers.NewCredentialsHandler(database, encKey)
	if err != nil {
		log.Fatalf("invalid credential encryption key: %v", err)
	}

	sysCredHandler, err := handlers.NewSystemCredentialsHandler(database, encKey)
	if err != nil {
		log.Fatalf("invalid credential encryption key for system credentials: %v", err)
	}

	// Knowledge store
	knowledgeStore := knowledge.NewDBStore(database)

	// MCP handler
	mcpHandler := handlers.NewMCPHandler(database, knowledgeStore)

	// Notify listener — fan-outs PostgreSQL LISTEN/NOTIFY to SSE subscribers.
	nl := notify.New(envOr("DATABASE_URL", "postgres://fleetlift:fleetlift@localhost:5432/fleetlift"))
	go func() {
		if err := nl.Start(ctx); err != nil && ctx.Err() == nil {
			log.Printf("notify listener exited: %v", err)
		}
	}()

	// Build router
	deps := server.Deps{
		JWTSecret:         jwtSecret,
		TemporalUIURL:     envOr("TEMPORAL_UI_URL", "http://localhost:8233"),
		Auth:              handlers.NewAuthHandler(database, ghProvider, jwtSecret),
		Workflows:         handlers.NewWorkflowsHandler(registry),
		Runs:              handlers.NewRunsHandler(database, temporalClient, registry, nl),
		Inbox:             handlers.NewInboxHandler(database, temporalClient),
		Reports:           handlers.NewReportsHandler(database),
		Credentials:       credHandler,
		SystemCredentials: sysCredHandler,
		Knowledge:         handlers.NewKnowledgeHandler(knowledgeStore),
		MCP:               mcpHandler,
		DB:                database,
		Actions:           handlers.NewActionsHandler(model.DefaultActionRegistry()),
		Profiles:          handlers.NewProfilesHandler(database),
	}

	handler, err := server.NewRouter(deps)
	if err != nil {
		log.Fatalf("build router: %v", err)
	}

	addr := envOr("LISTEN_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute, // generous for SSE
	}

	go func() {
		log.Printf("fleetlift server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
