package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImprovePrompt_InvalidInput(t *testing.T) {
	h := &PromptHandlers{
		Improve: func(ctx context.Context, prompt string) (*improveResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}

	cases := []struct {
		name string
		body string
	}{
		{"empty prompt", `{"prompt": ""}`},
		{"whitespace only", `{"prompt": "   "}`},
		{"malformed JSON", `not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/prompt/improve", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ImprovePrompt(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestImprovePrompt_Success(t *testing.T) {
	mockResp := &improveResponse{
		Improved: "Improved prompt text here",
		Scores: improveScores{
			Clarity:   scoreDetail{Rating: "excellent", Reason: "very clear"},
			Context:   scoreDetail{Rating: "good", Reason: "has some context"},
			Structure: scoreDetail{Rating: "poor", Reason: "unstructured"},
			Guidance:  scoreDetail{Rating: "good", Reason: "some guidance"},
		},
		Summary: "Improved clarity and structure",
	}

	h := &PromptHandlers{
		Improve: func(ctx context.Context, prompt string) (*improveResponse, error) {
			if prompt != "fix the bug" {
				t.Fatalf("unexpected prompt: %s", prompt)
			}
			return mockResp, nil
		},
	}

	body := `{"prompt": "fix the bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/prompt/improve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ImprovePrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp improveResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.Improved != "Improved prompt text here" {
		t.Fatalf("unexpected improved: %s", resp.Improved)
	}
	if resp.Summary != "Improved clarity and structure" {
		t.Fatalf("unexpected summary: %s", resp.Summary)
	}

	// Verify all four score dimensions.
	validRatings := map[string]bool{"excellent": true, "good": true, "poor": true}
	scores := map[string]scoreDetail{
		"clarity":   resp.Scores.Clarity,
		"context":   resp.Scores.Context,
		"structure": resp.Scores.Structure,
		"guidance":  resp.Scores.Guidance,
	}
	for name, score := range scores {
		if !validRatings[score.Rating] {
			t.Fatalf("invalid rating for %s: %s", name, score.Rating)
		}
		if score.Reason == "" {
			t.Fatalf("empty reason for %s", name)
		}
	}
}

func TestImprovePrompt_ImproverError(t *testing.T) {
	h := &PromptHandlers{
		Improve: func(ctx context.Context, prompt string) (*improveResponse, error) {
			return nil, fmt.Errorf("API unavailable")
		},
	}

	body := `{"prompt": "fix the bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/prompt/improve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ImprovePrompt(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == "" {
		t.Fatal("expected error message")
	}
}

func TestImprovePrompt_NilImprover(t *testing.T) {
	// No Improve function set and no DB configured → 503
	h := &PromptHandlers{}

	body := `{"prompt": "fix the bug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/prompt/improve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ImprovePrompt(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
