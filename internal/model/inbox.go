package model

import (
	"time"

	"github.com/lib/pq"
)

type InboxItem struct {
	ID         string         `db:"id" json:"id"`
	TeamID     string         `db:"team_id" json:"team_id"`
	RunID      string         `db:"run_id" json:"run_id"`
	StepRunID  *string        `db:"step_run_id" json:"step_run_id,omitempty"`
	Kind       string         `db:"kind" json:"kind"` // "awaiting_input" | "output_ready" | "notify" | "request_input"
	Title      string         `db:"title" json:"title"`
	Summary    *string        `db:"summary" json:"summary,omitempty"`
	Question   *string        `db:"question" json:"question,omitempty"`
	Options    pq.StringArray `db:"options" json:"options,omitempty"`
	Answer     *string        `db:"answer" json:"answer,omitempty"`
	AnsweredAt *time.Time     `db:"answered_at" json:"answered_at,omitempty"`
	AnsweredBy *string        `db:"answered_by" json:"answered_by,omitempty"`
	Urgency    string         `db:"urgency" json:"urgency"`
	CreatedAt  time.Time      `db:"created_at" json:"created_at"`
}

type InboxRead struct {
	InboxItemID string    `db:"inbox_item_id" json:"inbox_item_id"`
	UserID      string    `db:"user_id" json:"user_id"`
	ReadAt      time.Time `db:"read_at" json:"read_at"`
}
