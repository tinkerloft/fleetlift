package model

import "sort"

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

// DefaultActionRegistry returns a registry populated with all builtin action contracts.
func DefaultActionRegistry() *ActionRegistry {
	r := NewActionRegistry()

	r.Register(ActionContract{
		Type:        "slack_notify",
		Description: "Send a Slack notification to a channel",
		Inputs: []FieldContract{
			{Name: "channel", Type: "string", Required: true, Description: "Slack channel"},
			{Name: "message", Type: "string", Required: true, Description: "Message text"},
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
		Type:        "github_assign",
		Description: "Assign an issue to a team member based on component",
		Inputs: []FieldContract{
			{Name: "repo_url", Type: "string", Required: true, Description: "GitHub repository URL"},
			{Name: "issue_number", Type: "int", Required: true, Description: "Issue number"},
			{Name: "component", Type: "string", Required: false, Description: "Component for routing"},
		},
		Outputs: []FieldContract{
			{Name: "status", Type: "string", Required: true, Description: "assigned | skipped"},
			{Name: "reason", Type: "string", Required: false, Description: "Reason if skipped"},
		},
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

	return r
}
