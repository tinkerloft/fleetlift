package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jmoiron/sqlx"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/server/handlers"
	"github.com/tinkerloft/fleetlift/web"
)

// Deps holds all handler groups and shared configuration for the server.
type Deps struct {
	JWTSecret         []byte
	Auth              *handlers.AuthHandler
	Workflows         *handlers.WorkflowsHandler
	Runs              *handlers.RunsHandler
	Inbox             *handlers.InboxHandler
	Reports           *handlers.ReportsHandler
	Credentials       *handlers.CredentialsHandler
	SystemCredentials *handlers.SystemCredentialsHandler
	Knowledge         *handlers.KnowledgeHandler
	MCP               *handlers.MCPHandler
	DB                *sqlx.DB
	Actions           *handlers.ActionsHandler
	TemporalUIURL     string
}

// securityHeaders adds common security-related HTTP response headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// NewRouter creates the HTTP router with all API routes.
func NewRouter(deps Deps) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(cors.Handler(corsOptions()))

	// Auth (public)
	r.Get("/auth/github", deps.Auth.HandleGitHubRedirect)
	r.Get("/auth/github/callback", deps.Auth.HandleGitHubCallback)

	// Dev-mode auto-login — only available when DEV_NO_AUTH=1.
	// The frontend Login page calls this to obtain a real JWT without GitHub OAuth.
	if os.Getenv("DEV_NO_AUTH") == "1" {
		jwtSecret := deps.JWTSecret
		r.Get("/api/auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
			userID := os.Getenv("DEV_USER_ID")
			teamID := os.Getenv("DEV_TEAM_ID")
			token, err := auth.IssueToken(jwtSecret, userID, map[string]string{teamID: "admin"}, false)
			if err != nil {
				http.Error(w, "failed to issue dev token", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
		})
	}

	// Health check (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Public config (no auth required)
	temporalUIURL := deps.TemporalUIURL
	r.Get("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"temporal_ui_url": temporalUIURL,
		})
	})

	// MCP API (separate auth — run-scoped JWT, not user JWT)
	r.Route("/api/mcp", func(r chi.Router) {
		r.Use(auth.MCPAuth(deps.JWTSecret, deps.DB))
		r.Get("/run", deps.MCP.HandleGetRun)
		r.Get("/steps/{stepID}/output", deps.MCP.HandleGetStepOutput)
		r.Get("/knowledge", deps.MCP.HandleGetKnowledge)
		r.Post("/artifacts", deps.MCP.HandleCreateArtifact)
		r.Post("/knowledge", deps.MCP.HandleAddLearning)
		r.Get("/knowledge/search", deps.MCP.HandleSearchKnowledge)
		r.Post("/progress", deps.MCP.HandleUpdateProgress)
		r.Post("/inbox/notify", deps.MCP.HandleInboxNotify)
		r.Post("/inbox/request_input", deps.MCP.HandleInboxRequestInput)
	})

	// Authenticated API
	r.Group(func(r chi.Router) {
		if os.Getenv("DEV_NO_AUTH") == "1" {
			r.Use(devAuthBypass(deps.JWTSecret))
		} else {
			r.Use(auth.Middleware(deps.JWTSecret))
		}

		// Identity
		r.Post("/auth/refresh", deps.Auth.HandleRefresh)
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
		r.Get("/api/runs/{id}/events", deps.Runs.Stream)       // SSE: live run events
		r.Get("/api/runs/steps/{id}/logs", deps.Runs.StepLogs) // SSE: step log stream
		r.Post("/api/runs/{id}/approve", deps.Runs.Approve)
		r.Post("/api/runs/{id}/reject", deps.Runs.Reject)
		r.Post("/api/runs/{id}/steer", deps.Runs.Steer)
		r.Post("/api/runs/{id}/cancel", deps.Runs.Cancel)

		// Inbox
		r.Get("/api/inbox", deps.Inbox.List)
		r.Post("/api/inbox/{id}/read", deps.Inbox.MarkRead)
		r.Post("/api/inbox/{id}/respond", deps.Inbox.Respond)

		// Reports
		r.Get("/api/reports", deps.Reports.List)
		r.Get("/api/reports/{runID}", deps.Reports.Get)
		r.Get("/api/reports/{runID}/export", deps.Reports.Export)
		r.Get("/api/reports/{runID}/artifacts", deps.Reports.Artifacts)

		// Credentials
		r.Get("/api/credentials", deps.Credentials.List)
		r.Post("/api/credentials", deps.Credentials.Set)
		r.Delete("/api/credentials/{name}", deps.Credentials.Delete)

		// System Credentials (admin only)
		r.Get("/api/system-credentials", deps.SystemCredentials.List)
		r.Post("/api/system-credentials", deps.SystemCredentials.Set)
		r.Delete("/api/system-credentials/{name}", deps.SystemCredentials.Delete)

		// Knowledge
		r.Get("/api/knowledge", deps.Knowledge.List)
		r.Patch("/api/knowledge/{id}", deps.Knowledge.UpdateStatus)
		r.Delete("/api/knowledge/{id}", deps.Knowledge.Delete)

		// Action types (registry)
		r.Get("/api/action-types", deps.Actions.List)
	})

	// Serve embedded React SPA
	spa, err := spaHandler()
	if err != nil {
		return nil, fmt.Errorf("web: %w", err)
	}
	r.Handle("/*", spa)
	return r, nil
}

// devAuthBypass is a middleware that skips JWT validation and injects claims
// for the first team found in the database. Only enabled when DEV_NO_AUTH=1.
func devAuthBypass(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try real auth first (if token is present, use it)
			if tokenVal, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				if claims, err := auth.ValidateToken(jwtSecret, tokenVal); err == nil {
					ctx := auth.SetClaimsInContext(r.Context(), claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			// Fall back to a dev claims object
			claims := &auth.Claims{
				UserID:    os.Getenv("DEV_USER_ID"),
				TeamRoles: map[string]string{os.Getenv("DEV_TEAM_ID"): "admin"},
			}
			ctx := auth.SetClaimsInContext(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func corsOptions() cors.Options {
	allowed := os.Getenv("CORS_ALLOWED_ORIGINS")
	origins := []string{"http://localhost:5173", "http://localhost:8080"}
	if allowed != "" {
		origins = strings.Split(allowed, ",")
	}
	return cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}
}

func spaHandler() (http.Handler, error) {
	fsys, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("failed to sub dist: %w", err)
	}
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the file if it exists; otherwise fall back to index.html for SPA routing.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := fsys.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fall back to index.html for client-side routing.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}), nil
}
