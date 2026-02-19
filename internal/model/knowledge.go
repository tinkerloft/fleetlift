package model

import "time"

// KnowledgeType classifies the kind of knowledge item.
type KnowledgeType string

const (
	// KnowledgeTypePattern is a reusable approach that worked.
	KnowledgeTypePattern KnowledgeType = "pattern"
	// KnowledgeTypeCorrection is extracted from steering (agent went wrong, was corrected).
	KnowledgeTypeCorrection KnowledgeType = "correction"
	// KnowledgeTypeGotcha is a non-obvious failure mode.
	KnowledgeTypeGotcha KnowledgeType = "gotcha"
	// KnowledgeTypeContext is repo-specific knowledge.
	KnowledgeTypeContext KnowledgeType = "context"
)

// KnowledgeSource describes how a knowledge item was created.
type KnowledgeSource string

const (
	KnowledgeSourceAutoCaptured      KnowledgeSource = "auto_captured"
	KnowledgeSourceSteeringExtracted KnowledgeSource = "steering_extracted"
	KnowledgeSourceManual            KnowledgeSource = "manual"
)

// KnowledgeOrigin links a knowledge item back to its source execution.
type KnowledgeOrigin struct {
	TaskID         string `json:"task_id" yaml:"task_id"`
	Repository     string `json:"repository,omitempty" yaml:"repository,omitempty"`
	SteeringPrompt string `json:"steering_prompt,omitempty" yaml:"steering_prompt,omitempty"`
	Iteration      int    `json:"iteration,omitempty" yaml:"iteration,omitempty"`
}

// KnowledgeItem is a reusable piece of knowledge extracted from a transformation.
type KnowledgeItem struct {
	ID          string           `json:"id" yaml:"id"`
	Type        KnowledgeType    `json:"type" yaml:"type"`
	Summary     string           `json:"summary" yaml:"summary"`
	Details     string           `json:"details" yaml:"details"`
	Source      KnowledgeSource  `json:"source" yaml:"source"`
	Tags        []string         `json:"tags,omitempty" yaml:"tags,omitempty"`
	Confidence  float64          `json:"confidence" yaml:"confidence"`
	CreatedFrom *KnowledgeOrigin `json:"created_from,omitempty" yaml:"created_from,omitempty"`
	CreatedAt   time.Time        `json:"created_at" yaml:"created_at"`
}

// KnowledgeConfig is the optional knowledge section in a Task YAML.
type KnowledgeConfig struct {
	// CaptureDisabled disables auto-capture after approval (default: false = capture enabled).
	CaptureDisabled bool `json:"capture_disabled,omitempty" yaml:"capture_disabled,omitempty"`
	// EnrichDisabled disables prompt enrichment before agent execution (default: false = enrich enabled).
	EnrichDisabled bool `json:"enrich_disabled,omitempty" yaml:"enrich_disabled,omitempty"`
	// MaxItems caps how many knowledge items are injected into the prompt (default: 10).
	MaxItems int `json:"max_items,omitempty" yaml:"max_items,omitempty"`
	// Tags are additional tags for filtering/matching knowledge items.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}
