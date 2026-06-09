// Package query contains governance-manager repository filters.
package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

// PageRequest is an offset pagination request.
type PageRequest struct {
	PageSize  int32
	PageToken string
}

// PageResult is an offset pagination response.
type PageResult struct {
	NextPageToken string
}

// RiskProfileFilter filters risk profiles by scope and status.
type RiskProfileFilter struct {
	Scope  value.ExternalRef
	Status enum.RiskProfileStatus
	Page   PageRequest
}

// RuleFilter filters profile-version rules.
type RuleFilter struct {
	RiskProfileID  uuid.UUID
	ProfileVersion int64
	RuleKind       enum.RiskRuleKind
	Status         enum.RuleStatus
	Page           PageRequest
}

// GatePolicyFilter filters profile-version gate policies.
type GatePolicyFilter struct {
	RiskProfileID  uuid.UUID
	ProfileVersion int64
	GateKind       enum.GateKind
	Status         enum.RuleStatus
	Page           PageRequest
}

// RiskAssessmentFilter filters risk assessments.
type RiskAssessmentFilter struct {
	Target             value.ExternalRef
	ProjectContext     value.ProjectContextRef
	EffectiveRiskClass enum.RiskClass
	Status             enum.RiskAssessmentStatus
	Page               PageRequest
}

// RiskFactorFilter filters risk factors by assessment and source.
type RiskFactorFilter struct {
	RiskAssessmentID uuid.UUID
	SourceType       enum.RiskFactorSourceType
	Page             PageRequest
}

// ReviewSignalFilter filters review signals.
type ReviewSignalFilter struct {
	RiskAssessmentID *uuid.UUID
	Target           value.ExternalRef
	ProjectContext   value.ProjectContextRef
	RoleKind         enum.ReviewRoleKind
	Outcome          enum.ReviewSignalOutcome
	Page             PageRequest
}

// GateRequestFilter filters gate requests.
type GateRequestFilter struct {
	RiskAssessmentID *uuid.UUID
	Target           value.ExternalRef
	ProjectContext   value.ProjectContextRef
	Status           enum.GateRequestStatus
	Page             PageRequest
}

// GateDecisionFilter filters gate decisions.
type GateDecisionFilter struct {
	GateRequestID  *uuid.UUID
	Target         value.ExternalRef
	ProjectContext value.ProjectContextRef
	Outcome        enum.GateOutcome
	Page           PageRequest
}

// ReleaseDecisionPackageFilter filters release evidence packages.
type ReleaseDecisionPackageFilter struct {
	ProjectContext      value.ProjectContextRef
	ReleaseCandidateRef string
	IntegrationRef      value.ReleaseIntegrationRef
	Status              enum.ReleaseDecisionPackageStatus
	Page                PageRequest
}

// ReleaseDecisionFilter filters release decisions.
type ReleaseDecisionFilter struct {
	ReleaseDecisionPackageID *uuid.UUID
	ProjectContext           value.ProjectContextRef
	Status                   enum.ReleaseDecisionStatus
	Outcome                  enum.ReleaseDecisionOutcome
	Page                     PageRequest
}

// BlockingSignalFilter filters blocking signals by target and state.
type BlockingSignalFilter struct {
	Target         value.ExternalRef
	ProjectContext value.ProjectContextRef
	Status         enum.BlockingSignalStatus
	Severity       enum.SignalSeverity
	Page           PageRequest
}

// CommandIdentity identifies a mutating command for idempotent replay.
type CommandIdentity struct {
	CommandID      *uuid.UUID
	IdempotencyKey string
	Operation      string
	Actor          value.Actor
}
