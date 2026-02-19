package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.client.ApproveWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.client.RejectWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.client.CancelWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

type steerRequest struct {
	Prompt string `json:"prompt"`
}

func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req steerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if err := s.client.SteerWorkflow(r.Context(), id, req.Prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "steering"})
}

type continueRequest struct {
	SkipRemaining bool `json:"skip_remaining"`
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req continueRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.client.ContinueWorkflow(r.Context(), id, req.SkipRemaining); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "continued"})
}
