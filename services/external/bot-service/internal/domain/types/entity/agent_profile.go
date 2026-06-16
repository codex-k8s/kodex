package entity

import "time"

type AgentProfile struct {
	ID                int64
	Name              string
	Role              string
	Description       string
	Enabled           bool
	OpenAIAccountName string
	GitHubAccountName string
	KubernetesAccess  string
	SandboxMode       string
	ConfigOverlay     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
