package activity

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/temporal"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// ProfileStore resolves agent profiles by team and name.
// Team-scoped profiles take precedence over system (team_id IS NULL) profiles.
type ProfileStore interface {
	GetProfile(ctx context.Context, teamID, name string) (*model.AgentProfile, error)
}

// ResolveAgentProfile fetches the baseline and optional workflow profile, then merges them.
func (a *Activities) ResolveAgentProfile(ctx context.Context, input workflow.ResolveProfileInput) (model.AgentProfileBody, error) {
	baseline, err := a.ProfileStore.GetProfile(ctx, input.TeamID, "baseline")
	if err != nil {
		return model.AgentProfileBody{}, fmt.Errorf("fetch baseline profile: %w", err)
	}
	var baselineBody *model.AgentProfileBody
	if baseline != nil {
		baselineBody = &baseline.Body
	}

	name := input.ProfileName
	if name == "" || name == "baseline" {
		return model.MergeProfiles(baselineBody, nil), nil
	}

	wp, err := a.ProfileStore.GetProfile(ctx, input.TeamID, name)
	if err != nil {
		return model.AgentProfileBody{}, fmt.Errorf("fetch profile %q: %w", name, err)
	}
	if wp == nil {
		return model.AgentProfileBody{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("agent profile %q not found for team %s", name, input.TeamID),
			"ProfileNotFound", nil,
		)
	}
	return model.MergeProfiles(baselineBody, &wp.Body), nil
}
