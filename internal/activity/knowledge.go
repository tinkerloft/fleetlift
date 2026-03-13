package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// CaptureKnowledge reads fleetlift-knowledge.json from the sandbox and persists items to the DB.
func (a *Activities) CaptureKnowledge(ctx context.Context, input model.CaptureKnowledgeInput) error {
	if a.KnowledgeStore == nil {
		slog.WarnContext(ctx, "KnowledgeStore not configured, skipping capture")
		return nil
	}

	data, err := a.Sandbox.ReadBytes(ctx, input.SandboxID, "fleetlift-knowledge.json")
	if err != nil {
		slog.InfoContext(ctx, "no fleetlift-knowledge.json found in sandbox", "sandbox_id", input.SandboxID)
		return nil
	}

	type rawItem struct {
		Type       string   `json:"type"`
		Summary    string   `json:"summary"`
		Details    string   `json:"details"`
		Tags       []string `json:"tags"`
		Confidence float64  `json:"confidence"`
	}
	var raw []rawItem
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse fleetlift-knowledge.json: %w", err)
	}

	var items []model.KnowledgeItem
	for _, r := range raw {
		if r.Summary == "" {
			continue
		}
		conf := r.Confidence
		if conf == 0 {
			conf = 1.0
		}
		items = append(items, model.KnowledgeItem{
			TeamID:             input.TeamID,
			WorkflowTemplateID: input.WorkflowTemplateID,
			StepRunID:          input.StepRunID,
			Type:               model.KnowledgeType(r.Type),
			Summary:            r.Summary,
			Details:            r.Details,
			Source:             model.KnowledgeSourceAutoCaptured,
			Tags:               r.Tags,
			Confidence:         conf,
			Status:             model.KnowledgeStatusPending,
		})
	}
	if len(items) > 0 {
		if err := a.KnowledgeStore.BatchSave(ctx, items); err != nil {
			slog.ErrorContext(ctx, "failed to batch save knowledge items", "error", err)
		}
	}

	return nil
}
