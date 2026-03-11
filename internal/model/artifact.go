package model

import "time"

type Artifact struct {
	ID          string    `db:"id" json:"id"`
	StepRunID   string    `db:"step_run_id" json:"step_run_id"`
	Name        string    `db:"name" json:"name"`
	Path        string    `db:"path" json:"path"`
	SizeBytes   int64     `db:"size_bytes" json:"size_bytes"`
	ContentType string    `db:"content_type" json:"content_type"`
	Storage     string    `db:"storage" json:"storage"` // "inline" | "object_store"
	Data        []byte    `db:"data" json:"data,omitempty"`
	ObjectKey   string    `db:"object_key" json:"object_key,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}
