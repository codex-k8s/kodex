// Package service contains governance-manager use-cases.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

// Service is the governance-manager application service boundary.
type Service struct {
	repository  governancerepo.Repository
	clock       Clock
	idGenerator IDGenerator
	authorizer  Authorizer
}

// Config contains explicit service dependencies.
type Config struct {
	Repository  governancerepo.Repository
	Clock       Clock
	IDGenerator IDGenerator
	Authorizer  Authorizer
}

const (
	aggregateGateDecision       = "gate_decision"
	aggregateGateRequest        = "gate_request"
	aggregateRiskProfileVersion = "risk_profile_version"
)

// New creates a governance-manager service with default clock and ids.
func New(repository governancerepo.Repository) *Service {
	return NewWithConfig(Config{Repository: repository, Clock: systemClock{}, IDGenerator: uuidGenerator{}})
}

// NewWithConfig creates a governance-manager service with explicit dependencies.
func NewWithConfig(cfg Config) *Service {
	return &Service{repository: cfg.Repository, clock: cfg.Clock, idGenerator: cfg.IDGenerator, authorizer: cfg.Authorizer}
}

// Ready reports whether the minimal service dependencies are composed.
func (s *Service) Ready() bool {
	return s != nil && s.repository != nil && s.repository.Ready() && s.clock != nil && s.idGenerator != nil && s.authorizer != nil
}

// BacklogOperation returns Unimplemented for stable contract operations outside this slice.
func (s *Service) BacklogOperation(_ context.Context, input BacklogOperationInput) error {
	if input.Operation == enum.Operation("") {
		return errs.ErrInvalidArgument
	}
	if !s.Ready() {
		return fmt.Errorf("%w: governance service is not configured", errs.ErrDependencyUnavailable)
	}
	return fmt.Errorf("%w: %s remains outside GOV-3 storage slice", errs.ErrNotImplemented, input.Operation)
}

// CreateRiskProfile creates risk profile metadata.
func (s *Service) CreateRiskProfile(ctx context.Context, input CreateRiskProfileInput) (entity.RiskProfile, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationCreateRiskProfile.String(), governanceevents.AggregateRiskProfile)
	if err != nil {
		return entity.RiskProfile{}, err
	}
	if replayed {
		profile, err := s.repository.GetRiskProfile(ctx, result.AggregateID)
		if err != nil {
			return entity.RiskProfile{}, err
		}
		if !sameExternalRef(profile.Scope, input.Scope) || profile.Slug != strings.TrimSpace(input.Slug) {
			return entity.RiskProfile{}, errs.ErrConflict
		}
		return profile, nil
	}
	now := s.clock.Now()
	profile := entity.RiskProfile{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:         input.Scope,
		Slug:          strings.TrimSpace(input.Slug),
		DisplayName:   input.DisplayName,
		Description:   input.Description,
		Status:        enum.RiskProfileStatusDraft,
	}
	if profile.Scope.Type == "" || profile.Scope.Ref == "" || profile.Slug == "" {
		return entity.RiskProfile{}, errs.ErrInvalidArgument
	}
	result = commandResult(input.Meta, enum.OperationCreateRiskProfile.String(), governanceevents.AggregateRiskProfile, profile.ID, now)
	if err := s.repository.CreateRiskProfile(ctx, profile, result); err != nil {
		return entity.RiskProfile{}, err
	}
	return profile, nil
}

// CreateRiskProfileVersion creates an immutable policy version.
func (s *Service) CreateRiskProfileVersion(ctx context.Context, input CreateRiskProfileVersionInput) (entity.RiskProfileVersion, error) {
	if input.RiskProfileID == uuid.Nil {
		return entity.RiskProfileVersion{}, errs.ErrInvalidArgument
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationCreateRiskProfileVersion.String(), aggregateRiskProfileVersion)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	if replayed {
		if result.AggregateID != input.RiskProfileID {
			return entity.RiskProfileVersion{}, errs.ErrConflict
		}
		profileVersion, err := profileVersionFromCommandResult(result)
		if err != nil {
			return entity.RiskProfileVersion{}, err
		}
		return s.repository.GetRiskProfileVersion(ctx, input.RiskProfileID, profileVersion)
	}
	now := s.clock.Now()
	contentDigest := strings.TrimSpace(input.ContentDigest)
	if contentDigest == "" {
		contentDigest = versionContentDigest(input.Rules, input.GatePolicies)
	}
	version := entity.RiskProfileVersion{
		RiskProfileID:  input.RiskProfileID,
		ProfileVersion: now.UnixNano(),
		Status:         enum.RiskProfileVersionStatusDraft,
		Rules:          input.Rules,
		GatePolicies:   input.GatePolicies,
		ContentDigest:  contentDigest,
		CreatedAt:      now,
	}
	for index := range version.GatePolicies {
		if version.GatePolicies[index].ID == uuid.Nil {
			version.GatePolicies[index].ID = s.idGenerator.New()
		}
		version.GatePolicies[index].RiskProfileID = &version.RiskProfileID
		version.GatePolicies[index].ProfileVersion = version.ProfileVersion
		if version.GatePolicies[index].Status == "" {
			version.GatePolicies[index].Status = enum.RuleStatusActive
		}
	}
	for index := range version.Rules {
		if version.Rules[index].ID == uuid.Nil {
			version.Rules[index].ID = s.idGenerator.New()
		}
		version.Rules[index].RiskProfileID = version.RiskProfileID
		version.Rules[index].ProfileVersion = version.ProfileVersion
		version.Rules[index].CreatedAt = now
		version.Rules[index].UpdatedAt = now
		if version.Rules[index].Status == "" {
			version.Rules[index].Status = enum.RuleStatusActive
		}
	}
	result = commandResultWithPayload(input.Meta, enum.OperationCreateRiskProfileVersion.String(), aggregateRiskProfileVersion, input.RiskProfileID, now, map[string]any{
		"profile_version": version.ProfileVersion,
	})
	if err := s.repository.CreateRiskProfileVersion(ctx, version, result); err != nil {
		return entity.RiskProfileVersion{}, err
	}
	return version, nil
}

// ActivateRiskProfileVersion activates one profile version for future assessments.
func (s *Service) ActivateRiskProfileVersion(ctx context.Context, input ActivateRiskProfileVersionInput) (entity.RiskProfileVersion, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationActivateRiskProfileVersion.String(), governanceevents.AggregateRiskProfile)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	if replayed {
		if result.AggregateID != input.RiskProfileID {
			return entity.RiskProfileVersion{}, errs.ErrConflict
		}
		profileVersion, err := profileVersionFromCommandResult(result)
		if err != nil {
			return entity.RiskProfileVersion{}, err
		}
		if profileVersion != input.ProfileVersion {
			return entity.RiskProfileVersion{}, errs.ErrConflict
		}
		return s.repository.GetRiskProfileVersion(ctx, input.RiskProfileID, profileVersion)
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	profile, err := s.repository.GetRiskProfile(ctx, input.RiskProfileID)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	if profile.Version != previousVersion {
		return entity.RiskProfileVersion{}, errs.ErrConflict
	}
	version, err := s.repository.GetRiskProfileVersion(ctx, input.RiskProfileID, input.ProfileVersion)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	now := s.clock.Now()
	activeVersion := input.ProfileVersion
	profile.ActiveVersion = &activeVersion
	profile.Status = enum.RiskProfileStatusActive
	profile.Version = previousVersion + 1
	profile.UpdatedAt = now
	version.Status = enum.RiskProfileVersionStatusActive
	version.ActivatedAt = &now
	result = commandResultWithPayload(input.Meta, enum.OperationActivateRiskProfileVersion.String(), governanceevents.AggregateRiskProfile, profile.ID, now, map[string]any{
		"profile_version": version.ProfileVersion,
	})
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventPolicyVersionActivated, governanceevents.AggregateRiskProfile, profile.ID, now, governanceevents.Payload{
		RiskProfileID:   profile.ID.String(),
		ProfileVersion:  version.ProfileVersion,
		RiskRuleCount:   int64(len(version.Rules)),
		GatePolicyCount: int64(len(version.GatePolicies)),
		Version:         profile.Version,
	})
	if err := s.repository.ActivateRiskProfileVersion(ctx, profile, previousVersion, version, result, event); err != nil {
		return entity.RiskProfileVersion{}, err
	}
	return version, nil
}

// ArchiveRiskProfile archives profile metadata.
func (s *Service) ArchiveRiskProfile(ctx context.Context, input ArchiveRiskProfileInput) (entity.RiskProfile, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationArchiveRiskProfile.String(), governanceevents.AggregateRiskProfile)
	if err != nil {
		return entity.RiskProfile{}, err
	}
	if replayed {
		if result.AggregateID != input.RiskProfileID {
			return entity.RiskProfile{}, errs.ErrConflict
		}
		return s.repository.GetRiskProfile(ctx, result.AggregateID)
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.RiskProfile{}, err
	}
	profile, err := s.repository.GetRiskProfile(ctx, input.RiskProfileID)
	if err != nil {
		return entity.RiskProfile{}, err
	}
	if profile.Version != previousVersion {
		return entity.RiskProfile{}, errs.ErrConflict
	}
	profile.Status = enum.RiskProfileStatusArchived
	profile.Version = previousVersion + 1
	profile.UpdatedAt = s.clock.Now()
	result = commandResult(input.Meta, enum.OperationArchiveRiskProfile.String(), governanceevents.AggregateRiskProfile, profile.ID, profile.UpdatedAt)
	if err := s.repository.ArchiveRiskProfile(ctx, profile, previousVersion, result); err != nil {
		return entity.RiskProfile{}, err
	}
	return profile, nil
}

func (s *Service) GetRiskProfile(ctx context.Context, id uuid.UUID) (entity.RiskProfile, error) {
	return s.repository.GetRiskProfile(ctx, id)
}

func (s *Service) GetRiskProfileVersion(ctx context.Context, id uuid.UUID, profileVersion int64) (entity.RiskProfileVersion, error) {
	return s.repository.GetRiskProfileVersion(ctx, id, profileVersion)
}

func (s *Service) ListRiskProfiles(ctx context.Context, input ListRiskProfilesInput) ([]entity.RiskProfile, query.PageResult, error) {
	return s.repository.ListRiskProfiles(ctx, input.Filter)
}

func (s *Service) ListRiskRules(ctx context.Context, input ListRiskRulesInput) ([]entity.RiskRule, query.PageResult, error) {
	return s.repository.ListRiskRules(ctx, input.Filter)
}

func (s *Service) ListGatePolicies(ctx context.Context, input ListGatePoliciesInput) ([]entity.GatePolicy, query.PageResult, error) {
	return s.repository.ListGatePolicies(ctx, input.Filter)
}

// EvaluateRisk stores a minimal assessment record without running the full classifier.
func (s *Service) EvaluateRisk(ctx context.Context, input EvaluateRiskInput) (entity.RiskAssessment, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	if replayed {
		assessment, err := s.repository.GetRiskAssessment(ctx, result.AggregateID)
		if err != nil {
			return entity.RiskAssessment{}, err
		}
		if !sameExternalRef(assessment.Target, input.Target) {
			return entity.RiskAssessment{}, errs.ErrConflict
		}
		return assessment, nil
	}
	if input.Target.Type == "" || input.Target.Ref == "" {
		return entity.RiskAssessment{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now()
	explanation := strings.TrimSpace(input.Meta.Reason)
	if explanation == "" {
		explanation = "storage-only assessment; classifier pending GOV-4"
	}
	assessment := entity.RiskAssessment{
		VersionedBase:      entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Target:             input.Target,
		ProjectContext:     input.ProjectContext,
		ProviderContext:    input.ProviderContext,
		AgentContext:       input.AgentContext,
		RuntimeContext:     input.RuntimeContext,
		InitialRiskClass:   enum.RiskClassR0,
		EffectiveRiskClass: enum.RiskClassR0,
		Status:             enum.RiskAssessmentStatusActive,
		Explanation:        explanation,
	}
	result = commandResult(input.Meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment, assessment.ID, now)
	events := []entity.OutboxEvent{
		outboxEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentRequested, governanceevents.AggregateRiskAssessment, assessment.ID, now, governanceevents.Payload{
			RiskAssessmentID: assessment.ID.String(),
			ProjectRef:       assessment.ProjectContext.ProjectRef,
			Status:           string(assessment.Status),
			Version:          assessment.Version,
		}),
		outboxEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentCompleted, governanceevents.AggregateRiskAssessment, assessment.ID, now, governanceevents.Payload{
			RiskAssessmentID:   assessment.ID.String(),
			InitialRiskClass:   string(assessment.InitialRiskClass),
			EffectiveRiskClass: string(assessment.EffectiveRiskClass),
			RiskFactorCount:    0,
			RequiredGateCount:  0,
			Status:             string(assessment.Status),
			Version:            assessment.Version,
		}),
	}
	if err := s.repository.CreateRiskAssessment(ctx, assessment, nil, result, events); err != nil {
		return entity.RiskAssessment{}, err
	}
	return assessment, nil
}

func (s *Service) GetRiskAssessment(ctx context.Context, id uuid.UUID) (entity.RiskAssessment, error) {
	return s.repository.GetRiskAssessment(ctx, id)
}

func (s *Service) ListRiskAssessments(ctx context.Context, input ListRiskAssessmentsInput) ([]entity.RiskAssessment, query.PageResult, error) {
	return s.repository.ListRiskAssessments(ctx, input.Filter)
}

func (s *Service) ListRiskFactors(ctx context.Context, input ListRiskFactorsInput) ([]entity.RiskFactor, query.PageResult, error) {
	return s.repository.ListRiskFactors(ctx, input.Filter)
}

// RecordReviewSignal stores a bounded review signal reference.
func (s *Service) RecordReviewSignal(ctx context.Context, input RecordReviewSignalInput) (entity.ReviewSignal, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationRecordReviewSignal.String(), governanceevents.AggregateReviewSignal)
	if err != nil {
		return entity.ReviewSignal{}, err
	}
	if replayed {
		signal, err := s.repository.GetReviewSignal(ctx, result.AggregateID)
		if err != nil {
			return entity.ReviewSignal{}, err
		}
		if !sameExternalRef(signal.Target, input.Target) || signal.AuthorRef != strings.TrimSpace(input.AuthorRef) {
			return entity.ReviewSignal{}, errs.ErrConflict
		}
		return signal, nil
	}
	if input.Target.Type == "" || input.Target.Ref == "" || strings.TrimSpace(input.AuthorRef) == "" {
		return entity.ReviewSignal{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now()
	signal := entity.ReviewSignal{
		ID:               s.idGenerator.New(),
		RiskAssessmentID: input.RiskAssessmentID,
		Target:           input.Target,
		RoleKind:         input.RoleKind,
		AuthorRef:        strings.TrimSpace(input.AuthorRef),
		Outcome:          input.Outcome,
		Severity:         input.Severity,
		Confidence:       input.Confidence,
		EvidenceRefs:     input.EvidenceRefs,
		Summary:          input.Summary,
		CreatedAt:        now,
	}
	result = commandResult(input.Meta, enum.OperationRecordReviewSignal.String(), governanceevents.AggregateReviewSignal, signal.ID, now)
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventReviewSignalRecorded, governanceevents.AggregateReviewSignal, signal.ID, now, governanceevents.Payload{
		ReviewSignalID: signal.ID.String(),
		Outcome:        string(signal.Outcome),
		Status:         string(signal.Severity),
	})
	if err := s.repository.RecordReviewSignal(ctx, signal, result, event); err != nil {
		return entity.ReviewSignal{}, err
	}
	return signal, nil
}

func (s *Service) ListReviewSignals(ctx context.Context, input ListReviewSignalsInput) ([]entity.ReviewSignal, query.PageResult, error) {
	return s.repository.ListReviewSignals(ctx, input.Filter)
}

// RequestGate stores a gate request without owning delivery retries.
func (s *Service) RequestGate(ctx context.Context, input RequestGateInput) (entity.GateRequest, error) {
	if input.Target.Type == "" || input.Target.Ref == "" {
		return entity.GateRequest{}, errs.ErrInvalidArgument
	}
	if err := requireCommand(input.Meta, enum.OperationRequestGate.String()); err != nil {
		return entity.GateRequest{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionGateRequest, gateTargetResource(input.Target)); err != nil {
		return entity.GateRequest{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationRequestGate.String(), aggregateGateRequest)
	if err != nil {
		return entity.GateRequest{}, err
	}
	if replayed {
		request, err := s.repository.GetGateRequest(ctx, result.AggregateID)
		if err != nil {
			return entity.GateRequest{}, err
		}
		if !sameExternalRef(request.Target, input.Target) {
			return entity.GateRequest{}, errs.ErrConflict
		}
		return request, nil
	}
	now := s.clock.Now()
	request := entity.GateRequest{
		VersionedBase:          entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		RiskAssessmentID:       input.RiskAssessmentID,
		GatePolicyID:           input.GatePolicyID,
		Target:                 input.Target,
		InteractionDeliveryRef: input.InteractionDeliveryRef,
		EvidenceRefs:           input.EvidenceRefs,
		EvidenceSummary:        input.EvidenceSummary,
		Status:                 enum.GateRequestStatusRequested,
	}
	result = commandResult(input.Meta, enum.OperationRequestGate.String(), aggregateGateRequest, request.ID, now)
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventGateRequested, governanceevents.AggregateGate, request.ID, now, governanceevents.Payload{
		GateRequestID:    request.ID.String(),
		RiskAssessmentID: optionalUUIDString(request.RiskAssessmentID),
		Status:           string(request.Status),
		Version:          request.Version,
	})
	if err := s.repository.CreateGateRequest(ctx, request, result, event); err != nil {
		return entity.GateRequest{}, err
	}
	return request, nil
}

// SubmitGateDecision stores a final gate decision and resolves the gate request.
func (s *Service) SubmitGateDecision(ctx context.Context, input SubmitGateDecisionInput) (entity.GateDecision, entity.GateRequest, error) {
	if input.GateRequestID == uuid.Nil {
		return entity.GateDecision{}, entity.GateRequest{}, errs.ErrInvalidArgument
	}
	if err := requireCommand(input.Meta, enum.OperationSubmitGateDecision.String()); err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionGateDecide, gateResource(input.GateRequestID)); err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision)
	if err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	if replayed {
		decision, err := s.repository.GetGateDecision(ctx, result.AggregateID)
		if err != nil {
			return entity.GateDecision{}, entity.GateRequest{}, err
		}
		if decision.GateRequestID != input.GateRequestID {
			return entity.GateDecision{}, entity.GateRequest{}, errs.ErrConflict
		}
		request, err := s.repository.GetGateRequest(ctx, decision.GateRequestID)
		if err != nil {
			return entity.GateDecision{}, entity.GateRequest{}, err
		}
		return decision, request, nil
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	request, err := s.repository.GetGateRequest(ctx, input.GateRequestID)
	if err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	now := s.clock.Now()
	if request.Version != previousVersion {
		return entity.GateDecision{}, entity.GateRequest{}, errs.ErrConflict
	}
	if err := ensureGateRequestOpen(request.Status); err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	request.Version = previousVersion + 1
	request.Status = enum.GateRequestStatusResolved
	request.UpdatedAt = now
	request.InteractionDeliveryRef = input.InteractionDeliveryRef
	decision := entity.GateDecision{
		ID:                s.idGenerator.New(),
		GateRequestID:     request.ID,
		DecisionActorRef:  strings.TrimSpace(input.DecisionActorRef),
		DecisionPolicyRef: strings.TrimSpace(input.DecisionPolicyRef),
		Outcome:           input.Outcome,
		Reason:            input.Reason,
		ConditionsSummary: input.ConditionsSummary,
		SourceRef:         input.SourceRef,
		DecidedAt:         now,
	}
	if decision.DecisionActorRef == "" {
		return entity.GateDecision{}, entity.GateRequest{}, errs.ErrInvalidArgument
	}
	result = commandResultWithPayload(input.Meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision, decision.ID, now, map[string]any{
		"gate_request_id": request.ID.String(),
	})
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventGateResolved, governanceevents.AggregateGate, request.ID, now, governanceevents.Payload{
		GateRequestID:    request.ID.String(),
		GateDecisionID:   decision.ID.String(),
		DecisionActorRef: decision.DecisionActorRef,
		Outcome:          string(decision.Outcome),
		Status:           string(request.Status),
		Version:          request.Version,
	})
	if err := s.repository.UpdateGateRequestWithDecision(ctx, request, previousVersion, decision, result, event); err != nil {
		return entity.GateDecision{}, entity.GateRequest{}, err
	}
	return decision, request, nil
}

// CancelGate records a terminal cancellation for an open gate request.
func (s *Service) CancelGate(ctx context.Context, input CancelGateInput) (entity.GateRequest, error) {
	return s.closeGateRequest(ctx, closeGateRequestInput{
		GateRequestID:          input.GateRequestID,
		Reason:                 input.Reason,
		InteractionDeliveryRef: input.InteractionDeliveryRef,
		Meta:                   input.Meta,
		Operation:              enum.OperationCancelGate,
		Status:                 enum.GateRequestStatusCancelled,
		EventType:              governanceevents.EventGateCancelled,
		ReasonCode:             "cancelled",
	})
}

// ExpireGate records a terminal expiry for an open gate request.
func (s *Service) ExpireGate(ctx context.Context, input ExpireGateInput) (entity.GateRequest, error) {
	closeInput := closeGateRequestInput{
		Operation:  enum.OperationExpireGate,
		Status:     enum.GateRequestStatusExpired,
		EventType:  governanceevents.EventGateExpired,
		ReasonCode: "expired",
	}
	closeInput.GateRequestID = input.GateRequestID
	closeInput.Reason = input.Reason
	closeInput.InteractionDeliveryRef = input.InteractionDeliveryRef
	closeInput.Meta = input.Meta
	return s.closeGateRequest(ctx, closeInput)
}

func (s *Service) GetGateRequest(ctx context.Context, input GetGateRequestInput) (entity.GateRequest, error) {
	if input.GateRequestID == uuid.Nil {
		return entity.GateRequest{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeGateRead(ctx, input.Meta, input.GateRequestID); err != nil {
		return entity.GateRequest{}, err
	}
	return s.repository.GetGateRequest(ctx, input.GateRequestID)
}

func (s *Service) GetGateDecision(ctx context.Context, input GetGateDecisionInput) (entity.GateDecision, error) {
	if input.GateDecisionID == uuid.Nil || input.GateRequestID == uuid.Nil {
		return entity.GateDecision{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeGateRead(ctx, input.Meta, input.GateRequestID); err != nil {
		return entity.GateDecision{}, err
	}
	decision, err := s.repository.GetGateDecision(ctx, input.GateDecisionID)
	if err != nil {
		return entity.GateDecision{}, err
	}
	if decision.GateRequestID != input.GateRequestID {
		return entity.GateDecision{}, errs.ErrNotFound
	}
	return decision, nil
}

func (s *Service) ListGateRequests(ctx context.Context, input ListGateRequestsInput) ([]entity.GateRequest, query.PageResult, error) {
	if err := s.authorizeGateTargetRead(ctx, input.Meta, input.Filter.Target); err != nil {
		return nil, query.PageResult{}, err
	}
	return s.repository.ListGateRequests(ctx, input.Filter)
}

func (s *Service) ListGateDecisions(ctx context.Context, input ListGateDecisionsInput) ([]entity.GateDecision, query.PageResult, error) {
	if input.Filter.GateRequestID != nil {
		if err := s.authorizeGateRead(ctx, input.Meta, *input.Filter.GateRequestID); err != nil {
			return nil, query.PageResult{}, err
		}
	} else if err := s.authorizeGateTargetRead(ctx, input.Meta, input.Filter.Target); err != nil {
		return nil, query.PageResult{}, err
	}
	return s.repository.ListGateDecisions(ctx, input.Filter)
}

// BuildReleaseDecisionPackage stores bounded release evidence refs.
func (s *Service) BuildReleaseDecisionPackage(ctx context.Context, input BuildReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationBuildReleaseDecisionPackage.String(), governanceevents.AggregateReleaseDecisionPackage)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	if replayed {
		item, err := s.repository.GetReleaseDecisionPackage(ctx, result.AggregateID)
		if err != nil {
			return entity.ReleaseDecisionPackage{}, err
		}
		if item.ReleaseCandidateRef != strings.TrimSpace(input.ReleaseCandidateRef) || item.ProjectContext.ProjectRef != strings.TrimSpace(input.ProjectContext.ProjectRef) {
			return entity.ReleaseDecisionPackage{}, errs.ErrConflict
		}
		return item, nil
	}
	if strings.TrimSpace(input.ReleaseCandidateRef) == "" || strings.TrimSpace(input.ProjectContext.ProjectRef) == "" {
		return entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now()
	item := entity.ReleaseDecisionPackage{
		VersionedBase:           entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ReleaseCandidateRef:     strings.TrimSpace(input.ReleaseCandidateRef),
		ProjectContext:          input.ProjectContext,
		RepositoryRefs:          input.RepositoryRefs,
		RiskAssessmentID:        input.RiskAssessmentID,
		ProviderRefs:            input.ProviderRefs,
		RuntimeRefs:             input.RuntimeRefs,
		AgentContext:            input.AgentContext,
		ReviewSignalIDs:         input.ReviewSignalIDs,
		EvidenceRefs:            input.EvidenceRefs,
		KnownLimitationsSummary: input.KnownLimitationsSummary,
		Status:                  enum.ReleaseDecisionPackageStatusReady,
	}
	result = commandResult(input.Meta, enum.OperationBuildReleaseDecisionPackage.String(), governanceevents.AggregateReleaseDecisionPackage, item.ID, now)
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventReleaseDecisionPackageBuilt, governanceevents.AggregateReleaseDecisionPackage, item.ID, now, governanceevents.Payload{
		ReleaseDecisionPackageID: item.ID.String(),
		ReleaseCandidateRef:      item.ReleaseCandidateRef,
		ProjectRef:               item.ProjectContext.ProjectRef,
		Status:                   string(item.Status),
		Version:                  item.Version,
	})
	if err := s.repository.CreateReleaseDecisionPackage(ctx, item, result, event); err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	return item, nil
}

func (s *Service) GetReleaseDecisionPackage(ctx context.Context, id uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	return s.repository.GetReleaseDecisionPackage(ctx, id)
}

func (s *Service) ListReleaseDecisionPackages(ctx context.Context, input ListReleaseDecisionPackagesInput) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	return s.repository.ListReleaseDecisionPackages(ctx, input.Filter)
}

type closeGateRequestInput struct {
	GateRequestID          uuid.UUID
	Reason                 string
	InteractionDeliveryRef value.InteractionDeliveryRef
	Meta                   CommandMeta
	Operation              enum.Operation
	Status                 enum.GateRequestStatus
	EventType              string
	ReasonCode             string
}

func (s *Service) closeGateRequest(ctx context.Context, input closeGateRequestInput) (entity.GateRequest, error) {
	if input.GateRequestID == uuid.Nil {
		return entity.GateRequest{}, errs.ErrInvalidArgument
	}
	if err := requireCommand(input.Meta, input.Operation.String()); err != nil {
		return entity.GateRequest{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionGateDecide, gateResource(input.GateRequestID)); err != nil {
		return entity.GateRequest{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, input.Operation.String(), aggregateGateRequest)
	if err != nil {
		return entity.GateRequest{}, err
	}
	if replayed {
		if result.AggregateID != input.GateRequestID {
			return entity.GateRequest{}, errs.ErrConflict
		}
		return s.repository.GetGateRequest(ctx, result.AggregateID)
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.GateRequest{}, err
	}
	request, err := s.repository.GetGateRequest(ctx, input.GateRequestID)
	if err != nil {
		return entity.GateRequest{}, err
	}
	if request.Version != previousVersion {
		return entity.GateRequest{}, errs.ErrConflict
	}
	if err := ensureGateRequestOpen(request.Status); err != nil {
		return entity.GateRequest{}, err
	}
	now := s.clock.Now()
	previousStatus := request.Status
	request.Version = previousVersion + 1
	request.Status = input.Status
	request.UpdatedAt = now
	request.TerminalActorRef = actorRef(input.Meta.Actor)
	request.TerminalReason = strings.TrimSpace(input.Reason)
	request.TerminalAt = &now
	if !emptyInteractionDeliveryRef(input.InteractionDeliveryRef) {
		request.InteractionDeliveryRef = input.InteractionDeliveryRef
	}
	result = commandResultWithPayload(input.Meta, input.Operation.String(), aggregateGateRequest, request.ID, now, map[string]any{
		"status": string(request.Status),
	})
	event := outboxEvent(s.idGenerator.New(), input.EventType, governanceevents.AggregateGate, request.ID, now, governanceevents.Payload{
		GateRequestID:  request.ID.String(),
		PreviousStatus: string(previousStatus),
		Status:         string(request.Status),
		ReasonCode:     input.ReasonCode,
		Version:        request.Version,
	})
	if err := s.repository.UpdateGateRequestStatus(ctx, request, previousVersion, result, event); err != nil {
		return entity.GateRequest{}, err
	}
	return request, nil
}

func (s *Service) authorizeGateRead(ctx context.Context, meta QueryMeta, gateRequestID uuid.UUID) error {
	return s.authorizeQuery(ctx, meta, actionGateRead, gateResource(gateRequestID))
}

func (s *Service) authorizeGateTargetRead(ctx context.Context, meta QueryMeta, target value.ExternalRef) error {
	return s.authorizeQuery(ctx, meta, actionGateRead, gateTargetResource(target))
}

func ensureGateRequestOpen(status enum.GateRequestStatus) error {
	switch status {
	case enum.GateRequestStatusRequested, enum.GateRequestStatusDelivering, enum.GateRequestStatusAwaitingDecision:
		return nil
	default:
		return errs.ErrPreconditionFailed
	}
}

func emptyInteractionDeliveryRef(ref value.InteractionDeliveryRef) bool {
	return strings.TrimSpace(ref.RequestRef) == "" &&
		strings.TrimSpace(ref.DeliveryRef) == "" &&
		strings.TrimSpace(ref.CallbackRef) == "" &&
		strings.TrimSpace(ref.DecisionRef) == ""
}

func actorRef(actor value.Actor) string {
	return strings.TrimSpace(actor.Type) + ":" + strings.TrimSpace(actor.ID)
}

func requireCommand(meta CommandMeta, operation string) error {
	if strings.TrimSpace(operation) == "" || strings.TrimSpace(meta.Actor.Type) == "" || strings.TrimSpace(meta.Actor.ID) == "" {
		return errs.ErrInvalidArgument
	}
	if (meta.CommandID == nil || *meta.CommandID == uuid.Nil) && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func requireExpectedVersion(meta CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion <= 0 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}

func (s *Service) replayCommand(ctx context.Context, meta CommandMeta, operation string, aggregateType string) (entity.CommandResult, bool, error) {
	if err := requireCommand(meta, operation); err != nil {
		return entity.CommandResult{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, query.CommandIdentity{
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Operation:      operation,
		Actor:          meta.Actor,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return entity.CommandResult{}, false, nil
	}
	if err != nil {
		return entity.CommandResult{}, false, err
	}
	if err := validateCommandReplay(result, meta, operation, aggregateType); err != nil {
		return entity.CommandResult{}, true, err
	}
	return result, true, nil
}

func validateCommandReplay(result entity.CommandResult, meta CommandMeta, operation string, aggregateType string) error {
	if result.Operation != operation || result.AggregateType != aggregateType || result.AggregateID == uuid.Nil {
		return errs.ErrConflict
	}
	if result.Actor.Type != meta.Actor.Type || result.Actor.ID != meta.Actor.ID {
		return errs.ErrConflict
	}
	if meta.CommandID != nil && *meta.CommandID != uuid.Nil {
		if result.CommandID == nil || *result.CommandID != *meta.CommandID {
			return errs.ErrConflict
		}
		return nil
	}
	if strings.TrimSpace(result.IdempotencyKey) != strings.TrimSpace(meta.IdempotencyKey) {
		return errs.ErrConflict
	}
	return nil
}

func commandResult(meta CommandMeta, operation string, aggregateType string, aggregateID uuid.UUID, now time.Time) entity.CommandResult {
	return commandResultWithPayload(meta, operation, aggregateType, aggregateID, now, nil)
}

func commandResultWithPayload(meta CommandMeta, operation string, aggregateType string, aggregateID uuid.UUID, now time.Time, payload map[string]any) entity.CommandResult {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["aggregate_id"] = aggregateID.String()
	resultPayload, _ := json.Marshal(payload)
	return entity.CommandResult{
		Key:            commandResultKey(meta, operation),
		CommandID:      meta.CommandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Actor:          meta.Actor,
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  resultPayload,
		CreatedAt:      now,
	}
}

func profileVersionFromCommandResult(result entity.CommandResult) (int64, error) {
	var payload struct {
		ProfileVersion int64 `json:"profile_version"`
	}
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
		return 0, fmt.Errorf("%w: invalid command result payload", errs.ErrConflict)
	}
	if payload.ProfileVersion <= 0 {
		return 0, errs.ErrConflict
	}
	return payload.ProfileVersion, nil
}

func commandResultKey(meta CommandMeta, operation string) string {
	if meta.CommandID != nil && *meta.CommandID != uuid.Nil {
		return "command:" + meta.CommandID.String()
	}
	return "idempotency:" + operation + ":" + meta.Actor.Type + ":" + meta.Actor.ID + ":" + strings.TrimSpace(meta.IdempotencyKey)
}

func outboxEvent(id uuid.UUID, eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time, payload governanceevents.Payload) entity.OutboxEvent {
	body, _ := json.Marshal(payload)
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			id,
			eventType,
			governanceevents.SchemaVersion,
			aggregateType,
			aggregateID,
			body,
			occurredAt,
			0,
		),
		NextAttemptAt: occurredAt,
	}
}

func optionalUUIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func sameExternalRef(left value.ExternalRef, right value.ExternalRef) bool {
	return strings.TrimSpace(left.Type) == strings.TrimSpace(right.Type) && strings.TrimSpace(left.Ref) == strings.TrimSpace(right.Ref)
}

func versionContentDigest(rules []entity.RiskRule, gatePolicies []entity.GatePolicy) string {
	payload, _ := json.Marshal(struct {
		Rules        []entity.RiskRule   `json:"rules"`
		GatePolicies []entity.GatePolicy `json:"gate_policies"`
	}{Rules: rules, GatePolicies: gatePolicies})
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
