package model

import (
	"fmt"
	"sort"
	"strings"
)

// FieldContract declares a single input or output field for an action.
type FieldContract struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "int", "bool", "array"
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// ActionContract declares the full contract for an action type.
type ActionContract struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Inputs      []FieldContract `json:"inputs"`
	Outputs     []FieldContract `json:"outputs"`
	Credentials []string        `json:"credentials,omitempty"`
}

// ActionRegistry holds the set of known action contracts.
type ActionRegistry struct {
	contracts map[string]ActionContract
}

// NewActionRegistry creates an empty registry.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{contracts: make(map[string]ActionContract)}
}

// Register adds a contract to the registry.
func (r *ActionRegistry) Register(c ActionContract) {
	r.contracts[c.Type] = c
}

// Get returns the contract for an action type, or false if unknown.
func (r *ActionRegistry) Get(actionType string) (ActionContract, bool) {
	c, ok := r.contracts[actionType]
	return c, ok
}

// Types returns all registered action type names, sorted.
func (r *ActionRegistry) Types() []string {
	out := make([]string, 0, len(r.contracts))
	for k := range r.contracts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// All returns all registered contracts, sorted by type name.
func (r *ActionRegistry) All() []ActionContract {
	types := r.Types()
	out := make([]ActionContract, 0, len(types))
	for _, t := range types {
		out = append(out, r.contracts[t])
	}
	return out
}

// ValidateConfig checks an action's config map against its declared contract.
// Returns a list of human-readable error strings. Empty means valid.
// Config values containing "{{" skip type checking (template expressions).
func (r *ActionRegistry) ValidateConfig(actionType string, config map[string]any) []string {
	contract, ok := r.contracts[actionType]
	if !ok {
		return []string{fmt.Sprintf("unknown action type %q", actionType)}
	}

	if config == nil {
		config = map[string]any{}
	}

	var errs []string

	// Build set of known input names
	knownInputs := make(map[string]FieldContract, len(contract.Inputs))
	for _, f := range contract.Inputs {
		knownInputs[f.Name] = f
	}

	// Check for unknown keys
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := knownInputs[k]; !ok {
			errs = append(errs, fmt.Sprintf("unknown config key %q; known keys: %s", k, inputNames(contract)))
		}
	}

	// Check required fields present + type check non-template values
	for _, f := range contract.Inputs {
		val, provided := config[f.Name]
		if !provided {
			if f.Required {
				errs = append(errs, fmt.Sprintf("required config key %q is missing", f.Name))
			}
			continue
		}
		// Skip type checking for template expressions
		if s, ok := val.(string); ok && strings.Contains(s, "{{") {
			continue
		}
		if err := checkFieldType(f, val); err != "" {
			errs = append(errs, err)
		}
	}

	return errs
}

// HasOutputField returns true if the contract declares an output field with the given name.
func (c ActionContract) HasOutputField(name string) bool {
	for _, f := range c.Outputs {
		if f.Name == name {
			return true
		}
	}
	return false
}

// OutputFieldNames returns a comma-separated list of output field names.
func (c ActionContract) OutputFieldNames() string {
	names := make([]string, len(c.Outputs))
	for i, f := range c.Outputs {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

func checkFieldType(f FieldContract, val any) string {
	switch f.Type {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Sprintf("config key %q expects type string, got %T", f.Name, val)
		}
	case "int":
		switch v := val.(type) {
		case int, int64, float64:
			if fv, ok := v.(float64); ok && fv != float64(int64(fv)) {
				return fmt.Sprintf("config key %q expects type int, got fractional float", f.Name)
			}
		default:
			return fmt.Sprintf("config key %q expects type int, got %T", f.Name, val)
		}
	case "bool":
		if _, ok := val.(bool); !ok {
			return fmt.Sprintf("config key %q expects type bool, got %T", f.Name, val)
		}
	case "array":
		switch val.(type) {
		case []any, []string:
			// ok
		default:
			return fmt.Sprintf("config key %q expects type array, got %T", f.Name, val)
		}
	}
	return ""
}

func inputNames(c ActionContract) string {
	names := make([]string, len(c.Inputs))
	for i, f := range c.Inputs {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

// DefaultActionRegistry returns a registry populated with all builtin action contracts.
func DefaultActionRegistry() *ActionRegistry {
	r := NewActionRegistry()

	r.Register(ActionContract{
		Type:        "slack_notify",
		Description: "Send a Slack notification to a channel",
		Inputs: []FieldContract{
			{Name: "channel", Type: "string", Required: true, Description: "Slack channel"},
			{Name: "message", Type: "string", Required: true, Description: "Message text"},
			{Name: "thread_ts", Type: "string", Required: false, Description: "Thread timestamp to reply in"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "sent | failed"},
			{Name: "channel", Type: "string", Required: true, Description: "Channel message was sent to"},
		},
		Credentials: []string{"SLACK_BOT_TOKEN"},
	})

	r.Register(ActionContract{
		Type:        "github_pr_review",
		Description: "Post a review comment on a GitHub pull request",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
			{Name: "summary", Type: "string", Required: true, Description: "Review summary text"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "posted | skipped | failed"},
			{Name: "review_id", Type: "int", Required: false, Description: "GitHub review ID"},
			{Name: "reason", Type: "string", Required: false, Description: "Reason when skipped"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	r.Register(ActionContract{
		Type:        "github_label",
		Description: "Add labels to a GitHub issue",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "issue_number", Type: "int", Required: true, Description: "Issue number"},
			{Name: "labels", Type: "array", Required: true, Description: "Labels to apply"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "labeled | failed"},
			{Name: "labels", Type: "array", Required: true, Description: "Applied labels"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	r.Register(ActionContract{
		Type:        "github_comment",
		Description: "Post a comment on a GitHub issue",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "issue_number", Type: "int", Required: true, Description: "Issue number"},
			{Name: "body", Type: "string", Required: true, Description: "Comment body"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "posted | failed"},
			{Name: "comment_id", Type: "int", Required: true, Description: "GitHub comment ID"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	// create_pr is a passthrough (returns skipped_in_action), but including it in the
	// registry means ValidateConfig catches typos in config keys for create_pr action steps.
	r.Register(ActionContract{
		Type:        "create_pr",
		Description: "Create a pull request (handled by dedicated activity, not ExecuteAction)",
		Inputs: []FieldContract{
			{Name: "branch_prefix", Type: "string", Required: false, Description: "Branch name prefix"},
			{Name: "title", Type: "string", Required: true, Description: "PR title"},
			{Name: "draft", Type: "bool", Required: false, Description: "Create as draft PR"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "skipped_in_action"},
		},
	})

	r.Register(ActionContract{
		Type:        "github_fetch_pr",
		Description: "Fetch PR metadata and unified diff from GitHub API (no sandbox)",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
		},
		Outputs: []FieldContract{
			{Name: "diff", Type: "string", Required: true, Description: "Unified diff of the PR"},
			{Name: "title", Type: "string", Required: true, Description: "PR title"},
			{Name: "base_branch", Type: "string", Required: true, Description: "Base branch name"},
			{Name: "changed_files", Type: "array", Required: true, Description: "List of changed file paths"},
			{Name: "additions", Type: "int", Required: true, Description: "Total lines added"},
			{Name: "deletions", Type: "int", Required: true, Description: "Total lines deleted"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	r.Register(ActionContract{
		Type:        "github_pr_review_inline",
		Description: "Post inline review comments on a GitHub PR using file line numbers and side (LEFT/RIGHT)",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "pr_number", Type: "int", Required: true, Description: "Pull request number"},
			{Name: "annotations", Type: "string", Required: true, Description: "JSON array of {file, line, side, body} objects"},
			{Name: "commit_id", Type: "string", Required: false, Description: "Commit SHA to attach review to (uses PR head if omitted)"},
		},
		Outputs: []FieldContract{
			{Name: "posted", Type: "int", Required: true, Description: "Number of annotations successfully posted"},
			{Name: "skipped", Type: "int", Required: true, Description: "Number of annotations skipped"},
			{Name: "skipped_details", Type: "array", Required: false, Description: "Details of skipped annotations"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	r.Register(ActionContract{
		Type:        "github_update_comment",
		Description: "Update the body of an existing GitHub issue/PR comment",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "comment_id", Type: "int", Required: true, Description: "Comment ID to update"},
			{Name: "body", Type: "string", Required: true, Description: "New comment body"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "updated | failed"},
			{Name: "comment_id", Type: "int", Required: true, Description: "Updated comment ID"},
		},
		Credentials: []string{"GITHUB_TOKEN"},
	})

	return r
}
