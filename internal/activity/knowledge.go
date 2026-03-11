// Package activity contains Temporal activity implementations.
// TODO: Knowledge capture activities will be adapted in Phase 9.
package activity

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
)

// KnowledgeActivities contains Temporal activities for knowledge capture and enrichment.
type KnowledgeActivities struct {
	Store *knowledge.Store
}

// NewKnowledgeActivities creates a new KnowledgeActivities instance.
func NewKnowledgeActivities(store *knowledge.Store) *KnowledgeActivities {
	return &KnowledgeActivities{Store: store}
}

// CaptureKnowledge is a placeholder for the knowledge capture activity.
func (a *KnowledgeActivities) CaptureKnowledge(ctx context.Context) error {
	_ = activity.GetLogger(ctx)
	return nil
}
