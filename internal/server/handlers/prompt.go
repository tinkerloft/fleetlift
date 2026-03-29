package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
}

// ImprovePrompt handles POST /api/prompt/improve.
func (h *PromptHandlers) ImprovePrompt(w http.ResponseWriter, r *http.Request) {
	if h.Improve == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "prompt improvement is not configured (ANTHROPIC_API_KEY not set)")
		return
	}

	var req improveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	resp, err := h.Improve(r.Context(), req.Prompt)
	if err != nil {
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
