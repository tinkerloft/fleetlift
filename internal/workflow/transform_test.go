package workflow

import (
	"testing"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// TestSubstitutePromptTemplate tests the template substitution function.
func TestSubstitutePromptTemplate(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		target  model.ForEachTarget
		want    string
		wantErr bool
	}{
		{
			name:   "simple substitution with Name and Context",
			prompt: "Analyze the {{.Name}} endpoint. Context: {{.Context}}",
			target: model.ForEachTarget{
				Name:    "users-api",
				Context: "Handles user authentication",
			},
			want:    "Analyze the users-api endpoint. Context: Handles user authentication",
			wantErr: false,
		},
		{
			name:   "prompt without template variables (no-op)",
			prompt: "Analyze all endpoints in the repository",
			target: model.ForEachTarget{
				Name:    "users-api",
				Context: "Handles user authentication",
			},
			want:    "Analyze all endpoints in the repository",
			wantErr: false,
		},
		{
			name:   "multiple occurrences of same variable",
			prompt: "Target: {{.Name}}. Processing {{.Name}} now. Done with {{.Name}}.",
			target: model.ForEachTarget{
				Name:    "orders-api",
				Context: "",
			},
			want:    "Target: orders-api. Processing orders-api now. Done with orders-api.",
			wantErr: false,
		},
		{
			name:   "empty context",
			prompt: "Target: {{.Name}}, Context: {{.Context}}",
			target: model.ForEachTarget{
				Name:    "payments-api",
				Context: "",
			},
			want:    "Target: payments-api, Context: ",
			wantErr: false,
		},
		{
			name:   "multiline context",
			prompt: "Target: {{.Name}}\nContext:\n{{.Context}}",
			target: model.ForEachTarget{
				Name: "health-api",
				Context: `Line 1
Line 2
Line 3`,
			},
			want:    "Target: health-api\nContext:\nLine 1\nLine 2\nLine 3",
			wantErr: false,
		},
		{
			name:    "invalid template syntax",
			prompt:  "Bad template: {{.Name",
			target:  model.ForEachTarget{Name: "test"},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitutePromptTemplate(tt.prompt, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Errorf("substitutePromptTemplate() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("substitutePromptTemplate() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("substitutePromptTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}
