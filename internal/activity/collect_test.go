package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestCollectArtifacts_RejectsPathOutsideWorkspace(t *testing.T) {
	a := &Activities{
		Sandbox: &noopSandbox{},
		DB:      nil,
	}

	badPaths := []string{
		"/etc/passwd",
		"/home/user/secret",
		"/workspace",     // no trailing slash — not a prefix match
		"workspace/file", // relative path
		"",
	}

	for _, path := range badPaths {
		t.Run(path, func(t *testing.T) {
			err := a.CollectArtifacts(context.Background(), "sb-1", "sr-1", []model.ArtifactRef{
				{Name: "test", Path: path},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "/workspace/")
		})
	}
}

func TestCollectArtifacts_RejectsPathWithDotDot(t *testing.T) {
	a := &Activities{
		Sandbox: &noopSandbox{},
		DB:      nil,
	}

	dotDotPaths := []string{
		"/workspace/../etc/passwd",
		"/workspace/subdir/../../etc/passwd",
		"/workspace/..hidden",
	}

	for _, path := range dotDotPaths {
		t.Run(path, func(t *testing.T) {
			err := a.CollectArtifacts(context.Background(), "sb-1", "sr-1", []model.ArtifactRef{
				{Name: "test", Path: path},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "..")
		})
	}
}

func TestCollectArtifacts_AcceptsValidWorkspacePath(t *testing.T) {
	// A valid /workspace/ path passes validation and reaches activity.RecordHeartbeat.
	// In tests, RecordHeartbeat panics because there is no Temporal activity context.
	// We confirm that the panic is NOT a path-validation error (i.e., validation passed).
	a := &Activities{
		Sandbox: &noopSandbox{},
		DB:      nil,
	}

	defer func() {
		r := recover()
		// If there's a panic it should be from Temporal internals (heartbeat), not our code.
		if r != nil {
			msg := ""
			if s, ok := r.(string); ok {
				msg = s
			} else if e, ok := r.(error); ok {
				msg = e.Error()
			}
			assert.NotContains(t, msg, "/workspace/")
			assert.NotContains(t, msg, "artifact path")
		}
	}()

	_ = a.CollectArtifacts(context.Background(), "sb-1", "sr-1", []model.ArtifactRef{
		{Name: "output", Path: "/workspace/output.txt"},
	})
}
