package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
)

func TestValidateManifest_Valid(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID: "task-1",
		Mode:   "transform",
		Repositories: []fleetproto.ManifestRepo{
			{Name: "svc", URL: "https://github.com/org/svc.git"},
		},
	}
	require.NoError(t, ValidateManifest(m))
}

func TestValidateManifest_MissingTaskID(t *testing.T) {
	m := &fleetproto.TaskManifest{Mode: "transform", Repositories: []fleetproto.ManifestRepo{{Name: "svc"}}}
	assert.ErrorContains(t, ValidateManifest(m), "task_id is required")
}

func TestValidateManifest_MissingMode(t *testing.T) {
	m := &fleetproto.TaskManifest{TaskID: "t", Repositories: []fleetproto.ManifestRepo{{Name: "svc"}}}
	assert.ErrorContains(t, ValidateManifest(m), "mode is required")
}

func TestValidateManifest_InvalidMode(t *testing.T) {
	m := &fleetproto.TaskManifest{TaskID: "t", Mode: "invalid", Repositories: []fleetproto.ManifestRepo{{Name: "svc"}}}
	assert.ErrorContains(t, ValidateManifest(m), "mode must be")
}

func TestValidateManifest_PathTraversal_Slash(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:       "t",
		Mode:         "transform",
		Repositories: []fleetproto.ManifestRepo{{Name: "../etc/passwd"}},
	}
	assert.ErrorContains(t, ValidateManifest(m), "must not contain '/'")
}

func TestValidateManifest_PathTraversal_DotDot(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:       "t",
		Mode:         "transform",
		Repositories: []fleetproto.ManifestRepo{{Name: "svc/../../escape"}},
	}
	assert.ErrorContains(t, ValidateManifest(m), "must not contain '/'")
}

func TestValidateManifest_ControlChars(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:       "t",
		Mode:         "transform",
		Repositories: []fleetproto.ManifestRepo{{Name: "svc\x00evil"}},
	}
	assert.ErrorContains(t, ValidateManifest(m), "control characters")
}

func TestValidateManifest_EmptyRepoName(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:       "t",
		Mode:         "transform",
		Repositories: []fleetproto.ManifestRepo{{Name: ""}},
	}
	assert.ErrorContains(t, ValidateManifest(m), "name is required")
}

func TestValidateManifest_Targets(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID: "t",
		Mode:   "transform",
		Targets: []fleetproto.ManifestRepo{
			{Name: "target/../escape"},
		},
	}
	assert.ErrorContains(t, ValidateManifest(m), "must not contain '/'")
}

func TestValidateManifest_TransformationRepo(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:         "t",
		Mode:           "transform",
		Transformation: &fleetproto.ManifestRepo{Name: "tools/../../bad"},
	}
	assert.ErrorContains(t, ValidateManifest(m), "must not contain '/'")
}

func TestValidateManifest_ForEach(t *testing.T) {
	m := &fleetproto.TaskManifest{
		TaskID:  "t",
		Mode:    "report",
		ForEach: []fleetproto.ForEachTarget{{Name: "../escape", Context: "ctx"}},
	}
	assert.ErrorContains(t, ValidateManifest(m), "must not contain '/'")
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "my-repo", false},
		{"valid_with_dots", "my.repo.v2", false},
		{"empty", "", true},
		{"slash", "a/b", true},
		{"dot-dot", "a..b", true},
		{"control", "a\tb", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeName(tt.input, "test")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
