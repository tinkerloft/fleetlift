package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tinkerloft/fleetlift/internal/auth"
	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestHandleGetRun_NilClaims(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/run", nil)
	w := httptest.NewRecorder()
	h.HandleGetRun(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "unauthorized" {
		t.Fatalf("expected error 'unauthorized', got %q", resp["error"])
	}
}

func TestHandleGetStepOutput_NilClaims(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/steps/step1/output", nil)
	w := httptest.NewRecorder()
	h.HandleGetStepOutput(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleCreateArtifact_NameRequired(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	body := `{"content": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/artifacts", strings.NewReader(body))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleCreateArtifact(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "name is required" {
		t.Fatalf("expected error 'name is required', got %q", resp["error"])
	}
}

func TestHandleCreateArtifact_ContentSizeLimit(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	// Create content just over 1MB.
	bigContent := strings.Repeat("x", 1024*1024+1)
	body, _ := json.Marshal(map[string]string{"name": "test.txt", "content": bigContent})
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/artifacts", bytes.NewReader(body))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleCreateArtifact(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp["error"], "1MB") {
		t.Fatalf("expected 1MB error, got %q", resp["error"])
	}
}

func TestHandleCreateArtifact_PathTraversal(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	body := `{"name": "../../etc/passwd", "content": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/artifacts", strings.NewReader(body))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleCreateArtifact(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp["error"], "..") {
		t.Fatalf("expected path traversal error, got %q", resp["error"])
	}
}

func TestHandleUpdateProgress_PercentageRange(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"negative", `{"percentage": -1}`, http.StatusBadRequest},
		{"over 100", `{"percentage": 101}`, http.StatusBadRequest},
		{"zero", `{"percentage": 0}`, 0},   // will pass validation, fail on DB (nil)
		{"100", `{"percentage": 100}`, 0},   // will pass validation, fail on DB (nil)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewMCPHandler(nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/mcp/progress", strings.NewReader(tt.body))
			ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
				TeamID: "team1",
				RunID:  "run1",
			})
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			func() {
				defer func() { _ = recover() }()
				h.HandleUpdateProgress(w, req)
			}()

			if tt.wantStatus != 0 && w.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantStatus == 0 && w.Code == http.StatusBadRequest {
				t.Fatalf("valid percentage %s rejected with 400: %s", tt.body, w.Body.String())
			}
		})
	}
}

func TestHandleAddLearning_InvalidType(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	body := `{"type": "invalid", "summary": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/knowledge", strings.NewReader(body))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleAddLearning(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp["error"], "type must be one of") {
		t.Fatalf("expected type validation error, got %q", resp["error"])
	}
}

func TestHandleAddLearning_SummaryRequired(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	body := `{"type": "pattern", "summary": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/knowledge", strings.NewReader(body))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleAddLearning(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "summary is required" {
		t.Fatalf("expected error 'summary is required', got %q", resp["error"])
	}
}

func TestHandleAddLearning_ValidTypes(t *testing.T) {
	// Verify that all valid types pass validation. Since we have no DB,
	// the handler will panic on activeStepRunID. We use recover to catch that
	// and confirm it got past validation.
	validTypes := []string{"pattern", "correction", "gotcha", "context"}
	for _, vt := range validTypes {
		t.Run(vt, func(t *testing.T) {
			h := NewMCPHandler(nil, nil)
			body, _ := json.Marshal(map[string]string{"type": vt, "summary": "test summary"})
			req := httptest.NewRequest(http.MethodPost, "/api/mcp/knowledge", bytes.NewReader(body))
			ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
				TeamID: "team1",
				RunID:  "run1",
			})
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			func() {
				defer func() { _ = recover() }() // nil db will panic; that's fine
				h.HandleAddLearning(w, req)
			}()

			// If it wrote 400 before panicking, validation failed.
			if w.Code == http.StatusBadRequest {
				t.Fatalf("type %q should be valid, got 400: %s", vt, w.Body.String())
			}
		})
	}
}

func TestHandleSearchKnowledge_UsesStore(t *testing.T) {
	store := knowledge.NewMemoryStore()
	// Pre-populate with an approved item.
	_, err := store.Save(context.Background(), model.KnowledgeItem{
		TeamID:     "team1",
		Type:       model.KnowledgeTypePattern,
		Summary:    "test pattern",
		Details:    "details here",
		Source:     model.KnowledgeSourceManual,
		Confidence: 0.9,
		Status:     model.KnowledgeStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Also add a pending item (should not be returned).
	_, err = store.Save(context.Background(), model.KnowledgeItem{
		TeamID:     "team1",
		Type:       model.KnowledgeTypeGotcha,
		Summary:    "pending gotcha",
		Source:     model.KnowledgeSourceManual,
		Confidence: 0.5,
		Status:     model.KnowledgeStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := NewMCPHandler(nil, store)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/knowledge/search?q=test&max=5", nil)
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleSearchKnowledge(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []model.KnowledgeItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Summary != "test pattern" {
		t.Fatalf("expected 'test pattern', got %q", resp.Items[0].Summary)
	}
}

func TestHandleSearchKnowledge_NilClaims(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/knowledge/search", nil)
	w := httptest.NewRecorder()
	h.HandleSearchKnowledge(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleSearchKnowledge_WithTags(t *testing.T) {
	store := knowledge.NewMemoryStore()
	_, err := store.Save(context.Background(), model.KnowledgeItem{
		TeamID:     "team1",
		Type:       model.KnowledgeTypePattern,
		Summary:    "tagged item",
		Source:     model.KnowledgeSourceManual,
		Tags:       []string{"go", "testing"},
		Confidence: 0.8,
		Status:     model.KnowledgeStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(context.Background(), model.KnowledgeItem{
		TeamID:     "team1",
		Type:       model.KnowledgeTypePattern,
		Summary:    "untagged item",
		Source:     model.KnowledgeSourceManual,
		Confidence: 0.7,
		Status:     model.KnowledgeStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := NewMCPHandler(nil, store)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/knowledge/search?tags=go&max=10", nil)
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleSearchKnowledge(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []model.KnowledgeItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Summary != "tagged item" {
		t.Fatalf("expected 'tagged item', got %q", resp.Items[0].Summary)
	}
}

func TestHandleUpdateProgress_NilClaims(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/progress", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.HandleUpdateProgress(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleCreateArtifact_InvalidBody(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/artifacts", strings.NewReader("not json"))
	ctx := auth.SetMCPClaimsInContext(req.Context(), &auth.MCPClaims{
		TeamID: "team1",
		RunID:  "run1",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleCreateArtifact(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetKnowledge_NilClaims(t *testing.T) {
	h := NewMCPHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/knowledge", nil)
	w := httptest.NewRecorder()
	h.HandleGetKnowledge(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
