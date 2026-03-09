// Package server provides the HTTP API server for the Fleetlift web UI.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/tinkerloft/fleetlift/internal/create"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
)

// Server is the HTTP API server.
type Server struct {
	router         chi.Router
	client         TemporalClient
	staticFS       fs.FS // pre-subbed FS rooted at index.html; nil in tests
	gatherer       prometheus.Gatherer
	allowedOrigins []string
	conversations  *create.ConversationStore
	knowledgeStore *knowledge.Store
}

// New creates a new Server. staticFS may be nil (disables static serving).
// gatherer may be nil (uses prometheus.DefaultGatherer).
// allowedOrigins may be nil or empty (defaults to ["*"]).
func New(client TemporalClient, staticFS fs.FS, gatherer prometheus.Gatherer, allowedOrigins []string) *Server {
	return NewWithKnowledge(client, staticFS, gatherer, allowedOrigins, "")
}

// NewWithKnowledge creates a new Server with an explicit knowledge store directory.
// If knowledgeDir is empty, the default store (~/.fleetlift/knowledge) is used.
// allowedOrigins may be nil or empty (defaults to ["*"]).
func NewWithKnowledge(client TemporalClient, staticFS fs.FS, gatherer prometheus.Gatherer, allowedOrigins []string, knowledgeDir string) *Server {
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}
	var ks *knowledge.Store
	if knowledgeDir != "" {
		ks = knowledge.NewStore(knowledgeDir)
	} else {
		ks = knowledge.DefaultStore()
	}
	s := &Server{
		client:         client,
		staticFS:       staticFS,
		gatherer:       gatherer,
		allowedOrigins: allowedOrigins,
		conversations:  create.NewConversationStore(30 * time.Minute),
		knowledgeStore: ks,
	}
	s.router = s.buildRouter()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: s.allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
	}))

	r.Get("/api/v1/health", s.handleHealth)
	r.Get("/api/v1/config", s.handleGetConfig)

	// Task creation & templates
	r.Post("/api/v1/create/chat", s.handleChatStream)
	r.Post("/api/v1/create/validate", s.handleValidateYAML)
	r.Post("/api/v1/tasks", s.handleSubmitTask)
	r.Get("/api/v1/templates", s.handleListTemplates)
	r.Get("/api/v1/templates/{name}", s.handleGetTemplate)
	r.Post("/api/v1/templates/{name}/apply", s.handleApplyTemplate)

	// Knowledge routes (bulk and commit before {id} to avoid routing ambiguity)
	r.Get("/api/v1/knowledge", s.handleListKnowledge)
	r.Post("/api/v1/knowledge/bulk", s.handleBulkKnowledge)
	r.Post("/api/v1/knowledge/commit", s.handleCommitKnowledge)
	r.Post("/api/v1/knowledge", s.handleCreateKnowledge)
	r.Get("/api/v1/knowledge/{id}", s.handleGetKnowledge)
	r.Put("/api/v1/knowledge/{id}", s.handleUpdateKnowledge)
	r.Delete("/api/v1/knowledge/{id}", s.handleDeleteKnowledge)

	// Task routes
	r.Get("/api/v1/tasks", s.handleListTasks)
	r.Get("/api/v1/tasks/inbox", s.handleGetInbox)
	r.Route("/api/v1/tasks/{id}", func(r chi.Router) {
		r.Get("/", s.handleGetTask)
		r.Get("/result", s.handleGetResult)
		r.Get("/diff", s.handleGetDiff)
		r.Get("/logs", s.handleGetLogs)
		r.Get("/steering", s.handleGetSteering)
		r.Get("/progress", s.handleGetProgress)
		r.Get("/events", s.handleTaskEvents)
		r.Post("/approve", s.handleApprove)
		r.Post("/reject", s.handleReject)
		r.Post("/cancel", s.handleCancel)
		r.Post("/steer", s.handleSteer)
		r.Post("/continue", s.handleContinue)
		r.Post("/retry", s.handleRetryTask)
	})

	// Metrics endpoint
	g := s.gatherer
	if g == nil {
		g = prometheus.DefaultGatherer
	}
	r.Get("/metrics", promhttp.HandlerFor(g, promhttp.HandlerOpts{}).ServeHTTP)

	// Static SPA (registered last so API routes take priority)
	if s.staticFS != nil {
		r.Handle("/*", s.buildStaticHandler())
	}

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	temporalUIURL := os.Getenv("TEMPORAL_UI_URL")
	if temporalUIURL == "" {
		temporalUIURL = "http://localhost:8233"
	}
	writeJSON(w, http.StatusOK, map[string]string{"temporal_ui_url": temporalUIURL})
}

func (s *Server) buildStaticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[1:] // strip leading /
		if path == "" {
			path = "index.html"
		}
		if _, err := s.staticFS.Open(path); err != nil {
			// SPA fallback: unknown paths serve index.html
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
