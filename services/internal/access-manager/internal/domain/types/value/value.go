// Package value contains immutable value objects used by access-manager.
package value

import (
	"time"

	"github.com/google/uuid"

	accessevents "github.com/codex-k8s/kodex/libs/go/platformevents/access"
)

// Actor identifies the principal that initiated a command.
type Actor struct {
	Type string
	ID   string
}

// RequestContext stores safe request metadata for audit and diagnostics.
type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

// CommandMeta carries idempotency, concurrency and audit metadata.
type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
	Reason          string
	RequestID       string
	RequestContext  RequestContext
	OccurredAt      time.Time
}

// SubjectRef references a subject without importing its aggregate model.
type SubjectRef struct {
	Type string
	ID   string
}

// ResourceRef references a protected resource in access checks.
type ResourceRef struct {
	Type string
	ID   string
}

// ScopeRef references the scope where a policy is evaluated.
type ScopeRef struct {
	Type string
	ID   string
}

// RuleExplanation describes one rule that affected an access decision.
type RuleExplanation struct {
	RuleID     uuid.UUID
	Effect     string
	Subject    SubjectRef
	ActionKey  string
	Scope      ScopeRef
	Priority   int
	ReasonCode string
}

// DecisionExplanation is the machine-readable explanation for an access decision.
type DecisionExplanation struct {
	Decision      string
	ReasonCode    string
	PolicyVersion int64
	MatchedRules  []RuleExplanation
}

// AccessEventPayload is generated from AsyncAPI and used by access events.
type AccessEventPayload = accessevents.Payload
