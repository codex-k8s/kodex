package entity

import "time"

type AgentProfile struct {
	ID                int64
	Name              string
	Role              string
	Description       string
	Enabled           bool
	OpenAIAccountName string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
