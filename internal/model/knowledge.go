package model

import (
	"time"

	"github.com/lib/pq"
)

// KnowledgeType classifies the kind of knowledge item.
type KnowledgeType string

const (
	KnowledgeTypePattern    KnowledgeType = "pattern"
	KnowledgeTypeCorrection KnowledgeType = "correction"
	KnowledgeTypeGotcha     KnowledgeType = "gotcha"
	KnowledgeTypeContext    KnowledgeType = "context"
)

// KnowledgeSource describes how a knowledge item was created.
type KnowledgeSource string

const (
	KnowledgeSourceAutoCaptured KnowledgeSource = "auto_captured"
	KnowledgeSourceManual       KnowledgeSource = "manual"
)

// KnowledgeStatus represents the curation state of a knowledge item.
type KnowledgeStatus string

const (
	KnowledgeStatusPending  KnowledgeStatus = "pending"
	KnowledgeStatusApproved KnowledgeStatus = "approved"
	KnowledgeStatusRejected KnowledgeStatus = "rejected"
)

// KnowledgeItem is a reusable piece of knowledge extracted from a step run.
type KnowledgeItem struct {
	ID                 string          `db:"id" json:"id"`
	TeamID             string          `db:"team_id" json:"team_id"`
	WorkflowTemplateID *string         `db:"workflow_template_id" json:"workflow_template_id,omitempty"`
	StepRunID          *string         `db:"step_run_id" json:"step_run_id,omitempty"`
	Type               KnowledgeType   `db:"type" json:"type"`
	Summary            string          `db:"summary" json:"summary"`
	Details            string          `db:"details" json:"details,omitempty"`
	Source             KnowledgeSource `db:"source" json:"source"`
	Tags               pq.StringArray  `db:"tags" json:"tags,omitempty"`
	Confidence         float64         `db:"confidence" json:"confidence"`
	Status             KnowledgeStatus `db:"status" json:"status"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
}

// KnowledgeDef is the optional knowledge config block in a StepDef YAML.
type KnowledgeDef struct {
	Capture  bool     `yaml:"capture,omitempty"`
	Enrich   bool     `yaml:"enrich,omitempty"`
	MaxItems int      `yaml:"max_items,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
}

// CaptureKnowledgeInput is the input for the CaptureKnowledge Temporal activity.
type CaptureKnowledgeInput struct {
	SandboxID          string `json:"sandbox_id"`
	TeamID             string `json:"team_id"`
	WorkflowTemplateID string `json:"workflow_template_id,omitempty"`
	StepRunID          string `json:"step_run_id,omitempty"`
}
