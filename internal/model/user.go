package model

import "time"

type User struct {
	ID            string    `db:"id" json:"id"`
	Email         *string   `db:"email" json:"email,omitempty"`
	Name          string    `db:"name" json:"name"`
	Provider      string    `db:"provider" json:"provider"`
	ProviderID    string    `db:"provider_id" json:"provider_id"`
	PlatformAdmin bool      `db:"platform_admin" json:"platform_admin"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}
