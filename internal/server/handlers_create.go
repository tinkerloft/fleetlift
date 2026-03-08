package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/create"
	"github.com/tinkerloft/fleetlift/internal/model"
)

type chatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
}

// handleChatStream handles POST /api/v1/create/chat with SSE streaming.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		writeError(w, http.StatusServiceUnavailable, "ANTHROPIC_API_KEY is not configured")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Get or create conversation
	conv := s.conversations.Get(req.ConversationID)
	isNew := conv == nil
	if isNew {
		conv = &create.Conversation{
			ID: uuid.New().String(),
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send conversation ID first
	sendSSE(w, flusher, "conversation", map[string]string{"id": conv.ID})

	// If new conversation, seed it with the opening message then stream the response
	userMsg := req.Message
	if isNew {
		// The first user message is the seed — Claude responds with its opening question
		userMsg = "Hello! I'd like to create a Fleetlift task. " + req.Message
	}

	client := anthropic.NewClient()
	fullReply, err := create.StreamConversationMessage(
		r.Context(), &client, conv, userMsg,
		func(text string) error {
			sendSSE(w, flusher, "delta", map[string]string{"text": text})
			return nil
		},
	)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	// Check if Claude generated YAML
	response := map[string]any{"done": true}
	if create.HasGenerationMarker(fullReply) {
		yamlStr := create.ExtractYAMLFromMarker(fullReply)
		response["yaml"] = yamlStr
		if _, valErr := create.ValidateTaskYAML(yamlStr); valErr != nil {
			response["yaml_warning"] = valErr.Error()
		}
	}

	s.conversations.Put(conv)
	sendSSE(w, flusher, "done", response)
}

type submitRequest struct {
	YAML string `json:"yaml"`
}

type submitResponse struct {
	WorkflowID string `json:"workflow_id"`
}

// handleSubmitTask handles POST /api/v1/tasks to validate and start a task.
func (s *Server) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.YAML == "" {
		writeError(w, http.StatusBadRequest, "yaml is required")
		return
	}

	task, err := create.ValidateTaskYAML(req.YAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Ensure version is set
	if task.Version == 0 {
		task.Version = model.SchemaVersion
	}

	// Generate ID if missing
	if task.ID == "" {
		task.ID = uuid.New().String()[:8]
	}

	workflowID, err := s.client.StartTransform(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, submitResponse{WorkflowID: workflowID})
}

// handleValidateYAML handles POST /api/v1/create/validate.
func (s *Server) handleValidateYAML(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	task, err := create.ValidateTaskYAML(req.YAML)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "task": task})
}

// handleListTemplates handles GET /api/v1/templates.
func (s *Server) handleListTemplates(w http.ResponseWriter, _ *http.Request) {
	// Return templates without content to keep response small
	templates := make([]create.Template, len(create.BuiltinTemplates))
	for i, t := range create.BuiltinTemplates {
		templates[i] = create.Template{Name: t.Name, Description: t.Description}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

// handleGetTemplate handles GET /api/v1/templates/{name}.
func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	for _, t := range create.BuiltinTemplates {
		if t.Name == name {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("template %q not found", name))
}

// handleApplyTemplate handles POST /api/v1/templates/{name}/apply.
func (s *Server) handleApplyTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var tmpl *create.Template
	for _, t := range create.BuiltinTemplates {
		if t.Name == name {
			tmpl = &t
			break
		}
	}
	if tmpl == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("template %q not found", name))
		return
	}

	var req struct {
		Repos []string `json:"repos"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	content := tmpl.Content
	if len(req.Repos) > 0 {
		var task model.Task
		if err := yaml.Unmarshal([]byte(content), &task); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse template")
			return
		}
		task.Repositories = nil
		for _, u := range req.Repos {
			task.Repositories = append(task.Repositories, model.NewRepository(u, "main", ""))
		}
		out, err := yaml.Marshal(&task)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal template")
			return
		}
		content = string(out)
	}

	writeJSON(w, http.StatusOK, map[string]string{"yaml": content})
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}
