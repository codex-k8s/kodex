package entity

import "time"

type AgentPromptTemplate struct {
	ID          int64
	ProfileName string
	TemplateKey string
	Body        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
