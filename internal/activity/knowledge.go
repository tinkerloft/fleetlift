package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// KnowledgeActivities contains Temporal activities for knowledge capture and enrichment.
type KnowledgeActivities struct {
	Store *knowledge.Store
}

// NewKnowledgeActivities creates a KnowledgeActivities using the default local store.
func NewKnowledgeActivities() *KnowledgeActivities {
	return &KnowledgeActivities{Store: knowledge.DefaultStore()}
}

// CaptureKnowledgeInput is the input for the CaptureKnowledge activity.
type CaptureKnowledgeInput struct {
	TaskID          string                   `json:"task_id"`
	OriginalPrompt  string                   `json:"original_prompt"`
	SteeringHistory []model.SteeringIteration `json:"steering_history,omitempty"`
	DiffSummary     string                   `json:"diff_summary,omitempty"`
	VerifiersPassed bool                     `json:"verifiers_passed"`
	RepoNames       []string                 `json:"repo_names,omitempty"`
}

// EnrichPromptInput is the input for the EnrichPrompt activity.
type EnrichPromptInput struct {
	OriginalPrompt         string   `json:"original_prompt"`
	FilterTags             []string `json:"filter_tags,omitempty"`
	MaxItems               int      `json:"max_items,omitempty"`
	TransformationRepoPath string   `json:"transformation_repo_path,omitempty"`
}

// rawKnowledgeItem is the JSON shape returned by Claude.
type rawKnowledgeItem struct {
	Type       string   `json:"type"`
	Summary    string   `json:"summary"`
	Details    string   `json:"details"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// jsonArrayRE matches a JSON array (possibly fenced in markdown code blocks).
var jsonArrayRE = regexp.MustCompile(`(?s)\[.*?\]`)

// CaptureKnowledge calls Claude to extract reusable knowledge items from a completed
// transformation and writes them to the local store. It is non-blocking on failure:
// errors are logged as warnings and nil, nil is returned so the workflow is not disrupted.
func (ka *KnowledgeActivities) CaptureKnowledge(ctx context.Context, input CaptureKnowledgeInput) ([]model.KnowledgeItem, error) {
	prompt := BuildCapturePrompt(input)

	client := anthropic.NewClient()
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 2048,
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{OfText: &anthropic.TextBlockParam{Text: prompt}},
				},
			},
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "knowledge capture: Claude API call failed", "task_id", input.TaskID, "err", err)
		return nil, nil
	}

	var rawText string
	for _, block := range msg.Content {
		if block.Type == "text" {
			rawText += block.Text
		}
	}

	items, err := ParseKnowledgeItems(rawText, input.TaskID, input.SteeringHistory)
	if err != nil {
		slog.WarnContext(ctx, "knowledge capture: failed to parse items", "task_id", input.TaskID, "err", err)
		return nil, nil
	}

	for _, item := range items {
		if writeErr := ka.Store.Write(input.TaskID, item); writeErr != nil {
			slog.WarnContext(ctx, "knowledge capture: failed to write item", "task_id", input.TaskID, "item_id", item.ID, "err", writeErr)
		}
	}

	slog.InfoContext(ctx, "knowledge capture: captured items", "task_id", input.TaskID, "count", len(items))
	return items, nil
}

// EnrichPrompt loads knowledge items from Tier 3 (repo) then Tier 2 (local store),
// prepends a knowledge section to the original prompt, and returns the enriched prompt.
// Returns the original prompt unchanged if no items are found.
func (ka *KnowledgeActivities) EnrichPrompt(ctx context.Context, input EnrichPromptInput) (string, error) {
	maxItems := input.MaxItems
	if maxItems <= 0 {
		maxItems = 10
	}

	var items []model.KnowledgeItem

	// Tier 3: repo-level knowledge
	if input.TransformationRepoPath != "" {
		repoItems, err := knowledge.LoadFromRepo(input.TransformationRepoPath)
		if err != nil {
			slog.WarnContext(ctx, "enrich prompt: failed to load repo knowledge", "repo", input.TransformationRepoPath, "err", err)
		} else {
			items = append(items, repoItems...)
		}
	}

	// Tier 2: local store (filtered by tags)
	storeItems, err := ka.Store.FilterByTags(input.FilterTags, maxItems)
	if err != nil {
		slog.WarnContext(ctx, "enrich prompt: failed to load store knowledge", "err", err)
	} else {
		items = append(items, storeItems...)
	}

	// Cap total
	if len(items) > maxItems {
		items = items[:maxItems]
	}

	if len(items) == 0 {
		return input.OriginalPrompt, nil
	}

	var sb strings.Builder
	sb.WriteString(input.OriginalPrompt)
	sb.WriteString("\n\n---\n## Lessons from previous runs\n\nKeep these in mind based on previous transformations:\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", string(item.Type), item.Summary))
	}

	return sb.String(), nil
}

// BuildCapturePrompt constructs the prompt sent to Claude for knowledge extraction.
// It is exported so it can be tested independently.
func BuildCapturePrompt(input CaptureKnowledgeInput) string {
	var sb strings.Builder

	sb.WriteString("You are a knowledge extraction assistant. Analyze the following code transformation and extract reusable knowledge items.\n\n")
	sb.WriteString(fmt.Sprintf("Task ID: %s\n", input.TaskID))
	sb.WriteString(fmt.Sprintf("Original prompt: %s\n", input.OriginalPrompt))

	if len(input.SteeringHistory) > 0 {
		sb.WriteString("\n## Steering Corrections\n")
		for _, s := range input.SteeringHistory {
			sb.WriteString(fmt.Sprintf("- Iteration %d: %q → %q\n", s.IterationNumber, s.Prompt, s.Output))
		}
	}

	if input.DiffSummary != "" {
		sb.WriteString(fmt.Sprintf("\nDiff summary: %s\n", input.DiffSummary))
	}

	sb.WriteString(fmt.Sprintf("Verifiers passed: %v\n", input.VerifiersPassed))

	sb.WriteString(`
Extract knowledge items as a JSON array. Each item must have this shape (KnowledgeItem):
[
  {
    "type": "pattern|correction|gotcha|context",
    "summary": "one-line summary",
    "details": "explanation",
    "tags": ["tag1", "tag2"],
    "confidence": 0.0-1.0
  }
]

Return ONLY the JSON array, no other text.`)

	return sb.String()
}

// ParseKnowledgeItems parses Claude's JSON response into KnowledgeItem values,
// assigning IDs and Sources. Handles JSON wrapped in markdown code fences.
// It is exported so it can be tested independently.
func ParseKnowledgeItems(rawJSON, taskID string, steeringHistory []model.SteeringIteration) ([]model.KnowledgeItem, error) {
	cleaned := extractJSONArray(rawJSON)

	var raw []rawKnowledgeItem
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("parsing knowledge items JSON: %w", err)
	}

	hasSteeringHistory := len(steeringHistory) > 0
	now := time.Now()

	items := make([]model.KnowledgeItem, 0, len(raw))
	for _, r := range raw {
		kt := model.KnowledgeType(r.Type)
		source := model.KnowledgeSourceAutoCaptured
		if hasSteeringHistory && kt == model.KnowledgeTypeCorrection {
			source = model.KnowledgeSourceSteeringExtracted
		}

		id := uuid.New().String()[:8]

		items = append(items, model.KnowledgeItem{
			ID:         id,
			Type:       kt,
			Summary:    r.Summary,
			Details:    r.Details,
			Tags:       r.Tags,
			Confidence: r.Confidence,
			Source:     source,
			CreatedFrom: &model.KnowledgeOrigin{
				TaskID: taskID,
			},
			CreatedAt: now,
		})
	}

	return items, nil
}

// extractJSONArray strips markdown code fences and extracts the first JSON array.
func extractJSONArray(s string) string {
	s = strings.TrimSpace(s)

	// Strip markdown fences: ```json ... ``` or ``` ... ```
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx:]
		// remove opening fence line
		if nl := strings.Index(s, "\n"); nl >= 0 {
			s = s[nl+1:]
		}
		// remove closing fence
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}

	// If it starts with '[' it's already clean
	if strings.HasPrefix(s, "[") {
		return s
	}

	// Try to find a JSON array anywhere in the text
	if loc := jsonArrayRE.FindString(s); loc != "" {
		return loc
	}

	return s
}
