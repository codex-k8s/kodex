// Package entity contains governance-manager domain entities.
package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

// RiskPolicySnapshot is the MVP storage shape for the policy version used by one evaluation.
type RiskPolicySnapshot struct {
	ID             uuid.UUID
	Scope          value.ExternalRef
	ProjectRef     string
	RepositoryRef  string
	ProfileRef     string
	ProfileVersion int64
	CapturedAt     time.Time
}

// EvaluationRecord is the MVP storage shape for a requested or completed risk evaluation.
type EvaluationRecord struct {
	ID                 uuid.UUID
	Target             value.ExternalRef
	PolicySnapshotID   uuid.UUID
	InitialRiskClass   enum.RiskClass
	EffectiveRiskClass enum.RiskClass
	Status             enum.EvaluationStatus
	EvidenceRefs       []value.EvidenceRef
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// DecisionAuditRef is the MVP storage shape for gate/release decision audit references.
type DecisionAuditRef struct {
	ID                 uuid.UUID
	Kind               enum.DecisionAuditKind
	DecisionRef        string
	EvaluationRecordID uuid.UUID
	ActorRef           value.ExternalRef
	Reason             string
	EvidenceRefs       []value.EvidenceRef
	DecidedAt          time.Time
}
