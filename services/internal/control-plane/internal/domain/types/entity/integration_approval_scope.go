package entity

// IntegrationApprovalScopeContract exposes tool identity and upper bounds for human consent, not a stored consent or execution authorization.
// IntegrationApprovalScopeContract описывает tool identity и верхние границы human consent, не сохранённое согласование либо разрешение исполнения.
type IntegrationApprovalScopeContract struct {
	Name               string   `json:"name"`
	RequiredInputPaths []string `json:"requiredInputPaths"`
	MaxDurationSeconds int64    `json:"maxDurationSeconds"`
	MaxEffects         int64    `json:"maxEffects"`
	AllowedLifetimes   []string `json:"allowedLifetimes"`
}
