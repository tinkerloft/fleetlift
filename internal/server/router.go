package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
)

// Deps holds all handler groups and shared configuration for the server.
type Deps struct {
	JWTSecret   []byte
	Auth        *handlers.AuthHandler
	Workflows   *handlers.WorkflowsHandler
	Runs        *handlers.RunsHandler
	Inbox       *handlers.InboxHandler
	Reports     *handlers.ReportsHandler
	Credentials *handlers.CredentialsHandler
	Knowledge   *handlers.KnowledgeHandler
}

// NewRouter creates the HTTP router with all API routes.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(corsOptions()))

	// Auth (public)
	r.Get("/auth/github", deps.Auth.HandleGitHubRedirect)
	r.Get("/auth/github/callback", deps.Auth.HandleGitHubCallback)
	r.Post("/auth/refresh", deps.Auth.HandleRefresh)

	// Authenticated API
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(deps.JWTSecret))

		// Identity
		r.Get("/api/me", deps.Auth.HandleMe)

		// Workflows (templates)
		r.Get("/api/workflows", deps.Workflows.List)
		r.Get("/api/workflows/{id}", deps.Workflows.Get)
		r.Post("/api/workflows", deps.Workflows.Create)
		r.Put("/api/workflows/{id}", deps.Workflows.Update)
		r.Delete("/api/workflows/{id}", deps.Workflows.Delete)
		r.Post("/api/workflows/{id}/fork", deps.Workflows.Fork)

		// Runs
		r.Post("/api/runs", deps.Runs.Create)
		r.Get("/api/runs", deps.Runs.List)
		r.Get("/api/runs/{id}", deps.Runs.Get)
		r.Get("/api/runs/{id}/logs", deps.Runs.Logs)
		r.Get("/api/runs/{id}/diff", deps.Runs.Diff)
		r.Get("/api/runs/{id}/output", deps.Runs.Output)
		r.Get("/api/runs/{id}/events", deps.Runs.Stream) // SSE
		r.Post("/api/runs/{id}/approve", deps.Runs.Approve)
		r.Post("/api/runs/{id}/reject", deps.Runs.Reject)
		r.Post("/api/runs/{id}/steer", deps.Runs.Steer)
		r.Post("/api/runs/{id}/cancel", deps.Runs.Cancel)

		// Inbox
		r.Get("/api/inbox", deps.Inbox.List)
		r.Post("/api/inbox/{id}/read", deps.Inbox.MarkRead)

		// Reports
		r.Get("/api/reports", deps.Reports.List)
		r.Get("/api/reports/{runID}", deps.Reports.Get)
		r.Get("/api/reports/{runID}/export", deps.Reports.Export)

		// Credentials
		r.Get("/api/credentials", deps.Credentials.List)
		r.Post("/api/credentials", deps.Credentials.Set)
		r.Delete("/api/credentials/{name}", deps.Credentials.Delete)

		// Knowledge
		r.Get("/api/knowledge", deps.Knowledge.List)
		r.Patch("/api/knowledge/{id}", deps.Knowledge.UpdateStatus)
		r.Delete("/api/knowledge/{id}", deps.Knowledge.Delete)
	})

	// Serve embedded React SPA
	r.Handle("/*", spaHandler())

	return r
}

func corsOptions() cors.Options {
	return cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}
}

func spaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In production, this would serve the embedded React build.
		// For now, return a simple placeholder.
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>Fleetlift</h1><p>SPA not yet built.</p></body></html>`))
	})
}
