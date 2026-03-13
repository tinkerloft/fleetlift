package activity

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// artifactRow holds a single artifact's data ready for batch INSERT.
type artifactRow struct {
	name        string
	path        string
	sizeBytes   int
	contentType string
	storage     string
	data        []byte
}

// CollectArtifacts reads artifact files from a sandbox and stores them in the DB.
func (a *Activities) CollectArtifacts(ctx context.Context, sandboxID, stepRunID string, artifacts []model.ArtifactRef) error {
	rows := make([]artifactRow, 0, len(artifacts))

	for _, art := range artifacts {
		if !strings.HasPrefix(art.Path, "/workspace/") || strings.Contains(art.Path, "..") {
			return fmt.Errorf("artifact path %q must be within /workspace/ and must not contain ..", art.Path)
		}
		activity.RecordHeartbeat(ctx, "collecting "+art.Name)

		data, err := a.Sandbox.ReadBytes(ctx, sandboxID, art.Path)
		if err != nil {
			return fmt.Errorf("read artifact %s at %s: %w", art.Name, art.Path, err)
		}

		rows = append(rows, artifactRow{
			name:        art.Name,
			path:        art.Path,
			sizeBytes:   len(data),
			contentType: "text/plain",
			storage:     "inline",
			data:        data,
		})
	}

	if len(rows) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*7)
	for i, row := range rows {
		base := i * 7
		placeholders = append(placeholders, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7,
		))
		args = append(args, stepRunID, row.name, row.path, row.sizeBytes, row.contentType, row.storage, row.data)
	}
	query := `INSERT INTO artifacts (step_run_id, name, path, size_bytes, content_type, storage, data) VALUES ` +
		strings.Join(placeholders, ", ")
	if _, err := a.DB.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("store artifacts: %w", err)
	}
	return nil
}
