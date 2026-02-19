// Package server provides the HTTP API server for the Fleetlift web UI.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Server is the HTTP API server.
type Server struct {
	router   chi.Router
	client   TemporalClient
	staticFS fs.FS // pre-subbed FS rooted at index.html; nil in tests
}

// New creates a new Server. staticFS may be nil (disables static serving).
func New(client TemporalClient, staticFS fs.FS) *Server {
	s := &Server{client: client, staticFS: staticFS}
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
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
	}))

	r.Get("/api/v1/health", s.handleHealth)

	// Task routes
	r.Get("/api/v1/tasks", s.handleListTasks)
	r.Get("/api/v1/tasks/inbox", s.handleGetInbox)
	r.Route("/api/v1/tasks/{id}", func(r chi.Router) {
		r.Get("/", s.handleGetTask)
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
	})

	// Static SPA (registered last so API routes take priority)
	if s.staticFS != nil {
		r.Handle("/*", s.buildStaticHandler())
	}

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
