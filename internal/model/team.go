package model

import "time"

type Team struct {
	ID        string    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	Slug      string    `db:"slug" json:"slug"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type TeamMember struct {
	TeamID string `db:"team_id" json:"team_id"`
	UserID string `db:"user_id" json:"user_id"`
	Role   string `db:"role" json:"role"` // "member" | "admin"
}
