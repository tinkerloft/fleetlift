package activity

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// CollectArtifacts reads artifact files from a sandbox and stores them in the DB.
func (a *Activities) CollectArtifacts(ctx context.Context, sandboxID, stepRunID string, artifacts []model.ArtifactRef) error {
	for _, art := range artifacts {
		activity.RecordHeartbeat(ctx, "collecting "+art.Name)

		data, err := a.Sandbox.ReadBytes(ctx, sandboxID, art.Path)
		if err != nil {
			return fmt.Errorf("read artifact %s at %s: %w", art.Name, art.Path, err)
		}

		_, err = a.DB.ExecContext(ctx,
			`INSERT INTO artifacts (step_run_id, name, path, size_bytes, content_type, storage, data)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			stepRunID, art.Name, art.Path, len(data), "text/plain", "inline", data)
		if err != nil {
			return fmt.Errorf("store artifact %s: %w", art.Name, err)
		}
	}
	return nil
}
