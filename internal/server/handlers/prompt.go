package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	flcrypto "github.com/tinkerloft/fleetlift/internal/crypto"
)

const improveSystemPrompt = `You are an AI prompt quality analyst. Given a developer's prompt for a coding agent, you must:
1. Analyze the prompt quality across four dimensions: clarity, context, structure, guidance
2. Rewrite it as a structured, high-quality prompt
3. Rate each dimension as "excellent", "good", or "poor" with a brief reason

Respond ONLY with valid JSON matching this schema:
{
  "improved": "the rewritten prompt text",
  "scores": {
    "clarity":   { "rating": "excellent|good|poor", "reason": "brief reason" },
    "context":   { "rating": "excellent|good|poor", "reason": "brief reason" },
    "structure": { "rating": "excellent|good|poor", "reason": "brief reason" },
    "guidance":  { "rating": "excellent|good|poor", "reason": "brief reason" }
  },
  "summary": "one sentence summarizing the improvement"
}`

type improveRequest struct {
	Prompt string `json:"prompt"`
}

type scoreDetail struct {
	Rating string `json:"rating"`
	Reason string `json:"reason"`
}

type improveScores struct {
	Clarity   scoreDetail `json:"clarity"`
	Context   scoreDetail `json:"context"`
	Structure scoreDetail `json:"structure"`
	Guidance  scoreDetail `json:"guidance"`
}

type improveResponse struct {
	Improved string        `json:"improved"`
	Scores   improveScores `json:"scores"`
	Summary  string        `json:"summary"`
}

// PromptImprover is a function that takes a prompt and returns an improvement analysis.
type PromptImprover func(ctx context.Context, prompt string) (*improveResponse, error)

// PromptHandlers provides HTTP handlers for prompt-related endpoints.
type PromptHandlers struct {
	Improve PromptImprover

	// For system-credential based API key resolution.
	db            *sqlx.DB
	encryptionKey string

	mu          sync.RWMutex
	cachedKey   string
	cachedFn    PromptImprover
}

// NewPromptHandlers creates a PromptHandlers that resolves the Anthropic API key
// from the system credentials table (team_id IS NULL, name = 'ANTHROPIC_API_KEY').
// If Improve is set directly (e.g. in tests), it takes precedence.
func NewPromptHandlers(db *sqlx.DB, encryptionKey string) *PromptHandlers {
	return &PromptHandlers{db: db, encryptionKey: encryptionKey}
}

// resolveImprover looks up the ANTHROPIC_API_KEY from system credentials and
// returns a cached PromptImprover. Caches the client until the key changes.
func (h *PromptHandlers) resolveImprover(ctx context.Context) (PromptImprover, error) {
	if h.db == nil || h.encryptionKey == "" {
		return nil, fmt.Errorf("credential store not configured")
	}

	var valueEnc []byte
	err := h.db.GetContext(ctx, &valueEnc,
		`SELECT value_enc FROM credentials WHERE team_id IS NULL AND name = 'ANTHROPIC_API_KEY'`)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not found in system credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("query system credential: %w", err)
	}

	apiKey, err := flcrypto.DecryptAESGCM(h.encryptionKey, valueEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt ANTHROPIC_API_KEY: %w", err)
	}

	// Return cached improver if the key hasn't changed.
	h.mu.RLock()
	if h.cachedKey == apiKey && h.cachedFn != nil {
		fn := h.cachedFn
		h.mu.RUnlock()
		return fn, nil
	}
	h.mu.RUnlock()

	// Key changed or first call — create new client.
	fn := NewAnthropicImprover(apiKey)
	h.mu.Lock()
	h.cachedKey = apiKey
	h.cachedFn = fn
	h.mu.Unlock()
	return fn, nil
}

// ImprovePrompt handles POST /api/prompt/improve.
func (h *PromptHandlers) ImprovePrompt(w http.ResponseWriter, r *http.Request) {
	var req improveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Use directly-set Improve (tests), or resolve from system credentials.
	improve := h.Improve
	if improve == nil {
		var err error
		improve, err = h.resolveImprover(r.Context())
		if err != nil {
			slog.Error("failed to resolve prompt improvement credential", "error", err)
			writeJSONError(w, http.StatusServiceUnavailable, "prompt improvement is not configured")
			return
		}
	}

	resp, err := improve(r.Context(), req.Prompt)
	if err != nil {
		slog.Error("prompt improvement failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "prompt improvement failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// NewAnthropicImprover creates a PromptImprover that calls the Anthropic Messages API.
func NewAnthropicImprover(apiKey string) PromptImprover {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return func(ctx context.Context, prompt string) (*improveResponse, error) {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_0,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: improveSystemPrompt},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic API call failed: %w", err)
		}

		// Extract text from the first text content block.
		var text string
		for _, block := range msg.Content {
			if block.Type == "text" {
				text = block.Text
				break
			}
		}
		if text == "" {
			return nil, fmt.Errorf("no text content in response")
		}

		// Try to parse as JSON.
		var resp improveResponse
		if err := json.Unmarshal([]byte(text), &resp); err != nil {
			// Fallback: wrap raw text as improved prompt with default scores.
			return &improveResponse{
				Improved: text,
				Scores: improveScores{
					Clarity:   scoreDetail{Rating: "good", Reason: "could not parse structured response"},
					Context:   scoreDetail{Rating: "good", Reason: "could not parse structured response"},
					Structure: scoreDetail{Rating: "good", Reason: "could not parse structured response"},
					Guidance:  scoreDetail{Rating: "good", Reason: "could not parse structured response"},
				},
				Summary: "Prompt was improved but structured scoring was unavailable.",
			}, nil
		}

		return &resp, nil
	}
}
