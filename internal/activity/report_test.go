package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSchema(t *testing.T) {
	activities := &ReportActivities{}

	tests := []struct {
		name           string
		frontmatter    map[string]any
		schema         string
		wantErrors     bool
		wantErrorCount int
	}{
		{
			name: "valid frontmatter against schema",
			frontmatter: map[string]any{
				"title":  "Test Report",
				"status": "complete",
			},
			schema: `{
				"type": "object",
				"required": ["title", "status"],
				"properties": {
					"title": {"type": "string"},
					"status": {"type": "string", "enum": ["pending", "complete", "failed"]}
				}
			}`,
			wantErrors: false,
		},
		{
			name: "missing required field",
			frontmatter: map[string]any{
				"title": "Test Report",
			},
			schema: `{
				"type": "object",
				"required": ["title", "status"],
				"properties": {
					"title": {"type": "string"},
					"status": {"type": "string"}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "wrong type",
			frontmatter: map[string]any{
				"title": 123,
			},
			schema: `{
				"type": "object",
				"properties": {
					"title": {"type": "string"}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "invalid enum value",
			frontmatter: map[string]any{
				"status": "invalid",
			},
			schema: `{
				"type": "object",
				"properties": {
					"status": {"type": "string", "enum": ["pending", "complete"]}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			schema: `{
				"type": "object",
				"required": ["title"]
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "empty schema",
			frontmatter: map[string]any{
				"anything": "goes",
			},
			schema:     "",
			wantErrors: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ValidateSchemaInput{
				Frontmatter: tt.frontmatter,
				Schema:      tt.schema,
			}

			errors, err := activities.ValidateSchema(context.Background(), input)
			require.NoError(t, err)

			if tt.wantErrors {
				assert.NotEmpty(t, errors)
				if tt.wantErrorCount > 0 {
					assert.GreaterOrEqual(t, len(errors), tt.wantErrorCount)
				}
			} else {
				assert.Empty(t, errors)
			}
		})
	}
}
