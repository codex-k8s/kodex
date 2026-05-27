// Package governance contains governance-manager repository contracts.
package governance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
)

// Repository is the storage boundary for governance state.
type Repository interface {
	Ready() bool
	CreateRiskProfile(ctx context.Context, profile entity.RiskProfile, result entity.CommandResult) error
	CreateRiskProfileVersion(ctx context.Context, version entity.RiskProfileVersion, result entity.CommandResult) error
	ActivateRiskProfileVersion(ctx context.Context, profile entity.RiskProfile, previousProfileVersion int64, activatedVersion entity.RiskProfileVersion, result entity.CommandResult, event entity.OutboxEvent) error
	ArchiveRiskProfile(ctx context.Context, profile entity.RiskProfile, previousVersion int64, result entity.CommandResult) error
	GetRiskProfile(ctx context.Context, id uuid.UUID) (entity.RiskProfile, error)
	GetRiskProfileVersion(ctx context.Context, id uuid.UUID, profileVersion int64) (entity.RiskProfileVersion, error)
	ListRiskProfiles(ctx context.Context, filter query.RiskProfileFilter) ([]entity.RiskProfile, query.PageResult, error)
	ListRiskRules(ctx context.Context, filter query.RuleFilter) ([]entity.RiskRule, query.PageResult, error)
	ListGatePolicies(ctx context.Context, filter query.GatePolicyFilter) ([]entity.GatePolicy, query.PageResult, error)
	CreateRiskAssessment(ctx context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error
	UpdateRiskAssessment(ctx context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, previousVersion int64, result entity.CommandResult, events []entity.OutboxEvent) error
	GetRiskAssessment(ctx context.Context, id uuid.UUID) (entity.RiskAssessment, error)
	ListRiskAssessments(ctx context.Context, filter query.RiskAssessmentFilter) ([]entity.RiskAssessment, query.PageResult, error)
	ListRiskFactors(ctx context.Context, filter query.RiskFactorFilter) ([]entity.RiskFactor, query.PageResult, error)
	RecordReviewSignal(ctx context.Context, signal entity.ReviewSignal, result entity.CommandResult, event entity.OutboxEvent) error
	GetReviewSignal(ctx context.Context, id uuid.UUID) (entity.ReviewSignal, error)
	GetReviewSignalByFingerprint(ctx context.Context, fingerprint string) (entity.ReviewSignal, error)
	ListReviewSignals(ctx context.Context, filter query.ReviewSignalFilter) ([]entity.ReviewSignal, query.PageResult, error)
	CreateGateRequest(ctx context.Context, request entity.GateRequest, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateGateRequestStatus(ctx context.Context, request entity.GateRequest, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateGateRequestWithDecision(ctx context.Context, request entity.GateRequest, previousVersion int64, decision entity.GateDecision, result entity.CommandResult, event entity.OutboxEvent) error
	GetGateRequest(ctx context.Context, id uuid.UUID) (entity.GateRequest, error)
	GetGateDecision(ctx context.Context, id uuid.UUID) (entity.GateDecision, error)
	ListGateRequests(ctx context.Context, filter query.GateRequestFilter) ([]entity.GateRequest, query.PageResult, error)
	ListGateDecisions(ctx context.Context, filter query.GateDecisionFilter) ([]entity.GateDecision, query.PageResult, error)
	CreateReleaseDecisionPackage(ctx context.Context, item entity.ReleaseDecisionPackage, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateReleaseDecisionPackageStatus(ctx context.Context, item entity.ReleaseDecisionPackage, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetReleaseDecisionPackage(ctx context.Context, id uuid.UUID) (entity.ReleaseDecisionPackage, error)
	ListReleaseDecisionPackages(ctx context.Context, filter query.ReleaseDecisionPackageFilter) ([]entity.ReleaseDecisionPackage, query.PageResult, error)
	CreateReleaseDecision(ctx context.Context, pkg entity.ReleaseDecisionPackage, previousPackageVersion int64, decision entity.ReleaseDecision, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateReleaseDecision(ctx context.Context, pkg entity.ReleaseDecisionPackage, previousPackageVersion int64, decision entity.ReleaseDecision, previousDecisionVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetReleaseDecision(ctx context.Context, id uuid.UUID) (entity.ReleaseDecision, error)
	GetReleaseDecisionByPackage(ctx context.Context, releaseDecisionPackageID uuid.UUID) (entity.ReleaseDecision, error)
	ListReleaseDecisions(ctx context.Context, filter query.ReleaseDecisionFilter) ([]entity.ReleaseDecision, query.PageResult, error)
	RecordReleaseSafetyState(ctx context.Context, state entity.ReleaseSafetyState, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateReleaseSafetyState(ctx context.Context, state entity.ReleaseSafetyState, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetReleaseSafetyStateByPackage(ctx context.Context, releaseDecisionPackageID uuid.UUID) (entity.ReleaseSafetyState, error)
	RecordBlockingSignal(ctx context.Context, signal entity.BlockingSignal, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateBlockingSignal(ctx context.Context, signal entity.BlockingSignal, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetBlockingSignal(ctx context.Context, id uuid.UUID) (entity.BlockingSignal, error)
	ListBlockingSignals(ctx context.Context, filter query.BlockingSignalFilter) ([]entity.BlockingSignal, query.PageResult, error)
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	RecordCommandResult(ctx context.Context, result entity.CommandResult) error
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}
