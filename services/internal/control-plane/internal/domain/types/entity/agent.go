package entity

// Agent stores effective agent profile used by run orchestration.
type Agent struct {
	ID        string
	AgentKey  string
	RoleKind  string
	ProjectID string
	Name      string
	IsActive  bool
}
