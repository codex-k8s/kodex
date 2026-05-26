package enum

// RiskClass is the governance risk class used by stored assessments.
type RiskClass string

const (
	RiskClassR0 RiskClass = "R0"
	RiskClassR1 RiskClass = "R1"
	RiskClassR2 RiskClass = "R2"
	RiskClassR3 RiskClass = "R3"
)

// EvaluationStatus is the lifecycle status of a risk evaluation record.
type EvaluationStatus string

const (
	EvaluationStatusRequested EvaluationStatus = "requested"
	EvaluationStatusBacklog   EvaluationStatus = "backlog"
	EvaluationStatusCompleted EvaluationStatus = "completed"
)

// DecisionAuditKind classifies audit refs for gate and release decisions.
type DecisionAuditKind string

const (
	DecisionAuditKindGate    DecisionAuditKind = "gate"
	DecisionAuditKindRelease DecisionAuditKind = "release"
)
