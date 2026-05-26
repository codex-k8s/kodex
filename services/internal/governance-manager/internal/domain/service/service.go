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

	maxReleasePackageRefs      = 64
	maxReleasePackageJSONBytes = 16 * 1024

	releaseSafetyPreviousStatusNone = "none"
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

// EvaluateRisk stores a deterministic risk assessment produced by the local classifier.
func (s *Service) EvaluateRisk(ctx context.Context, input EvaluateRiskInput) (entity.RiskAssessment, error) {
	return s.evaluateRisk(ctx, input)
}

// ReevaluateRisk recalculates an existing assessment with optimistic concurrency.
func (s *Service) ReevaluateRisk(ctx context.Context, input ReevaluateRiskInput) (entity.RiskAssessment, error) {
	return s.reevaluateRisk(ctx, input)
}

func (s *Service) GetRiskAssessment(ctx context.Context, input GetRiskAssessmentInput) (entity.RiskAssessment, error) {
	return readByID(ctx, input.RiskAssessmentID, input.Meta, s.authorizeRiskAssessmentRead, s.repository.GetRiskAssessment)
}

func (s *Service) ListRiskAssessments(ctx context.Context, input ListRiskAssessmentsInput) ([]entity.RiskAssessment, query.PageResult, error) {
	if err := s.authorizeRiskAssessmentList(ctx, input.Meta, input.Filter); err != nil {
		return nil, query.PageResult{}, err
	}
	return s.repository.ListRiskAssessments(ctx, input.Filter)
}

func (s *Service) ListRiskFactors(ctx context.Context, input ListRiskFactorsInput) ([]entity.RiskFactor, query.PageResult, error) {
	if input.Filter.RiskAssessmentID == uuid.Nil {
		return nil, query.PageResult{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeRiskAssessmentRead(ctx, input.Meta, input.Filter.RiskAssessmentID); err != nil {
		return nil, query.PageResult{}, err
	}
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
	filter := input.Filter
	if err := s.authorizeReviewSignalList(ctx, input.Meta, filter); err != nil {
		return nil, query.PageResult{}, err
	}
	items, page, err := s.repository.ListReviewSignals(ctx, filter)
	if err != nil {
		return nil, query.PageResult{}, err
	}
	return items, page, nil
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
	return readByID(ctx, input.GateRequestID, input.Meta, s.authorizeGateRead, s.repository.GetGateRequest)
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
	return listWithAuthorization(ctx, input.Meta, input.Filter, s.authorizeGateRequestList, s.repository.ListGateRequests)
}

func (s *Service) ListGateDecisions(ctx context.Context, input ListGateDecisionsInput) ([]entity.GateDecision, query.PageResult, error) {
	return listWithAuthorization(ctx, input.Meta, input.Filter, s.authorizeGateDecisionList, s.repository.ListGateDecisions)
}

func listWithAuthorization[Item any, Filter any](
	ctx context.Context,
	meta QueryMeta,
	filter Filter,
	authorize func(context.Context, QueryMeta, Filter) error,
	list func(context.Context, Filter) ([]Item, query.PageResult, error),
) ([]Item, query.PageResult, error) {
	if err := authorize(ctx, meta, filter); err != nil {
		return nil, query.PageResult{}, err
	}
	return list(ctx, filter)
}

// BuildReleaseDecisionPackage stores bounded release evidence refs.
func (s *Service) BuildReleaseDecisionPackage(ctx context.Context, input BuildReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	releaseCandidateRef := strings.TrimSpace(input.ReleaseCandidateRef)
	if releaseCandidateRef == "" {
		return entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
	}
	if err := validateReleaseSafeRef("release_candidate_ref", releaseCandidateRef, true); err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	projectContext, err := normalizeReleaseProjectContext(input.ProjectContext)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	repositoryRefs, err := normalizeReleaseRepositoryRefs(input.RepositoryRefs)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	providerRefs, err := normalizeReleaseJSONArrayPayload("release.provider_refs", input.ProviderRefs)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	runtimeRefs, err := normalizeReleaseJSONArrayPayload("release.runtime_refs", input.RuntimeRefs)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	agentContext, err := normalizeReleaseJSONObjectPayload("release.agent_context", input.AgentContext)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	evidenceRefs, err := normalizeEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	knownLimitationsSummary, err := normalizeReleaseSafeText("release.known_limitations_summary", input.KnownLimitationsSummary, maxEvaluationSummaryLength)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionReleasePrepare, releaseDecisionContextResource(releaseCandidateRef)); err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationBuildReleaseDecisionPackage.String(), governanceevents.AggregateReleaseDecisionPackage)
	if err != nil {
		return entity.ReleaseDecisionPackage{}, err
	}
	if replayed {
		item, err := s.repository.GetReleaseDecisionPackage(ctx, result.AggregateID)
		if err != nil {
			return entity.ReleaseDecisionPackage{}, err
		}
		if item.ReleaseCandidateRef != releaseCandidateRef || item.ProjectContext.ProjectRef != projectContext.ProjectRef {
			return entity.ReleaseDecisionPackage{}, errs.ErrConflict
		}
		return item, nil
	}
	if input.RiskAssessmentID != nil {
		if _, err := s.repository.GetRiskAssessment(ctx, *input.RiskAssessmentID); err != nil {
			return entity.ReleaseDecisionPackage{}, err
		}
	}
	for _, reviewSignalID := range input.ReviewSignalIDs {
		if reviewSignalID == uuid.Nil {
			return entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
		}
		if _, err := s.repository.GetReviewSignal(ctx, reviewSignalID); err != nil {
			return entity.ReleaseDecisionPackage{}, err
		}
	}
	now := s.clock.Now()
	item := entity.ReleaseDecisionPackage{
		VersionedBase:           entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ReleaseCandidateRef:     releaseCandidateRef,
		ProjectContext:          projectContext,
		RepositoryRefs:          repositoryRefs,
		RiskAssessmentID:        input.RiskAssessmentID,
		ProviderRefs:            providerRefs,
		RuntimeRefs:             runtimeRefs,
		AgentContext:            agentContext,
		ReviewSignalIDs:         input.ReviewSignalIDs,
		EvidenceRefs:            evidenceRefs,
		KnownLimitationsSummary: knownLimitationsSummary,
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

func (s *Service) GetReleaseDecisionPackage(ctx context.Context, input GetReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	return readByID(ctx, input.ReleaseDecisionPackageID, input.Meta, s.authorizeReleaseRead, s.repository.GetReleaseDecisionPackage)
}

func (s *Service) ListReleaseDecisionPackages(ctx context.Context, input ListReleaseDecisionPackagesInput) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	return listWithAuthorization(ctx, input.Meta, input.Filter, s.authorizeReleasePackageList, s.repository.ListReleaseDecisionPackages)
}

// RequestReleaseDecision starts the minimal release decision lifecycle.
func (s *Service) RequestReleaseDecision(ctx context.Context, input RequestReleaseDecisionInput) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error) {
	if input.ReleaseDecisionPackageID == uuid.Nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionReleaseRequest, releaseDecisionResource(input.ReleaseDecisionPackageID)); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationRequestReleaseDecision.String(), governanceevents.AggregateReleaseDecision)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	if replayed {
		decision, pkg, err := s.replayedReleaseDecision(ctx, result.AggregateID)
		if err != nil {
			return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
		}
		if decision.ReleaseDecisionPackageID != input.ReleaseDecisionPackageID {
			return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrConflict
		}
		return decision, pkg, nil
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	pkg, err := s.repository.GetReleaseDecisionPackage(ctx, input.ReleaseDecisionPackageID)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	if pkg.Version != previousVersion {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrConflict
	}
	if pkg.Status != enum.ReleaseDecisionPackageStatusReady {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	reason, err := normalizeReleaseSafeText("release_decision.reason", input.Meta.Reason, maxEvaluationFactorSummary)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	pkg.Status = enum.ReleaseDecisionPackageStatusDecisionRequested
	pkg.Version = previousVersion + 1
	pkg.UpdatedAt = now
	decision := entity.ReleaseDecision{
		VersionedBase:            entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ReleaseDecisionPackageID: pkg.ID,
		DecisionActorRef:         actorRef(input.Meta.Actor),
		DecisionPolicyRef:        strings.TrimSpace(pkg.ProjectContext.ReleasePolicyRef),
		Reason:                   reason,
		Status:                   enum.ReleaseDecisionStatusRequested,
		DecidedAt:                now,
	}
	if decision.Reason == "" && input.RequestGateIfRequired {
		decision.Reason = "release_gate_requested"
	}
	result = commandResultWithPayload(input.Meta, enum.OperationRequestReleaseDecision.String(), governanceevents.AggregateReleaseDecision, decision.ID, now, map[string]any{
		"release_decision_package_id": pkg.ID.String(),
	})
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventReleaseDecisionRequested, governanceevents.AggregateReleaseDecision, decision.ID, now, governanceevents.Payload{
		ReleaseDecisionID:        decision.ID.String(),
		ReleaseDecisionPackageID: pkg.ID.String(),
		ReleaseCandidateRef:      pkg.ReleaseCandidateRef,
		ProjectRef:               pkg.ProjectContext.ProjectRef,
		Status:                   string(decision.Status),
		Version:                  decision.Version,
	})
	if err := s.repository.CreateReleaseDecision(ctx, pkg, previousVersion, decision, result, event); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	return decision, pkg, nil
}

// SubmitReleaseDecision resolves the current release decision for a package.
func (s *Service) SubmitReleaseDecision(ctx context.Context, input SubmitReleaseDecisionInput) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error) {
	if input.ReleaseDecisionPackageID == uuid.Nil || input.Outcome == "" {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionReleaseDecide, releaseDecisionResource(input.ReleaseDecisionPackageID)); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationSubmitReleaseDecision.String(), governanceevents.AggregateReleaseDecision)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	if replayed {
		decision, pkg, err := s.replayedReleaseDecision(ctx, result.AggregateID)
		if err != nil {
			return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
		}
		if decision.ReleaseDecisionPackageID != input.ReleaseDecisionPackageID || decision.Outcome != input.Outcome {
			return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrConflict
		}
		return decision, pkg, nil
	}
	previousDecisionVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	pkg, err := s.repository.GetReleaseDecisionPackage(ctx, input.ReleaseDecisionPackageID)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	decision, err := s.repository.GetReleaseDecisionByPackage(ctx, pkg.ID)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	if decision.Version != previousDecisionVersion {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrConflict
	}
	if decision.Status != enum.ReleaseDecisionStatusRequested || pkg.Status != enum.ReleaseDecisionPackageStatusDecisionRequested {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrPreconditionFailed
	}
	if err := s.ensureReleaseDecisionAllowed(ctx, pkg, input); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	decisionActorRef := strings.TrimSpace(input.DecisionActorRef)
	if err := validateReleaseSafeRef("release_decision.decision_actor_ref", decisionActorRef, true); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	decisionPolicyRef := strings.TrimSpace(input.DecisionPolicyRef)
	if err := validateReleaseSafeRef("release_decision.decision_policy_ref", decisionPolicyRef, false); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	reason, err := normalizeReleaseSafeText("release_decision.reason", input.Reason, maxEvaluationFactorSummary)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	conditionsSummary, err := normalizeReleaseSafeText("release_decision.conditions_summary", input.ConditionsSummary, maxEvaluationSummaryLength)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	now := s.clock.Now()
	previousPackageVersion := pkg.Version
	pkg.Status = enum.ReleaseDecisionPackageStatusClosed
	pkg.Version++
	pkg.UpdatedAt = now
	decision.GateDecisionID = input.GateDecisionID
	decision.Outcome = input.Outcome
	decision.DecisionActorRef = decisionActorRef
	decision.DecisionPolicyRef = decisionPolicyRef
	decision.Reason = reason
	decision.ConditionsSummary = conditionsSummary
	decision.Status = enum.ReleaseDecisionStatusResolved
	decision.Version = previousDecisionVersion + 1
	decision.DecidedAt = now
	decision.UpdatedAt = now
	if decision.DecisionActorRef == "" || decision.Reason == "" {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, errs.ErrInvalidArgument
	}
	result = commandResultWithPayload(input.Meta, enum.OperationSubmitReleaseDecision.String(), governanceevents.AggregateReleaseDecision, decision.ID, now, map[string]any{
		"release_decision_package_id": pkg.ID.String(),
		"outcome":                     string(decision.Outcome),
	})
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventReleaseDecisionResolved, governanceevents.AggregateReleaseDecision, decision.ID, now, governanceevents.Payload{
		ReleaseDecisionID:        decision.ID.String(),
		ReleaseDecisionPackageID: pkg.ID.String(),
		ReleaseCandidateRef:      pkg.ReleaseCandidateRef,
		ProjectRef:               pkg.ProjectContext.ProjectRef,
		DecisionActorRef:         decision.DecisionActorRef,
		Outcome:                  string(decision.Outcome),
		Status:                   string(decision.Status),
		Version:                  decision.Version,
	})
	if err := s.repository.UpdateReleaseDecision(ctx, pkg, previousPackageVersion, decision, previousDecisionVersion, result, event); err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	return decision, pkg, nil
}

func (s *Service) GetReleaseDecision(ctx context.Context, input GetReleaseDecisionInput) (entity.ReleaseDecision, error) {
	return readByID(ctx, input.ReleaseDecisionID, input.Meta, s.authorizeReleaseRead, s.repository.GetReleaseDecision)
}

func (s *Service) ListReleaseDecisions(ctx context.Context, input ListReleaseDecisionsInput) ([]entity.ReleaseDecision, query.PageResult, error) {
	return listWithAuthorization(ctx, input.Meta, input.Filter, s.authorizeReleaseDecisionList, s.repository.ListReleaseDecisions)
}

// RecordBlockingSignal stores a bounded blocking signal reference.
func (s *Service) RecordBlockingSignal(ctx context.Context, input RecordBlockingSignalInput) (entity.BlockingSignal, error) {
	target := value.ExternalRef{Type: strings.TrimSpace(input.Target.Type), Ref: strings.TrimSpace(input.Target.Ref)}
	if target.Type == "" || target.Ref == "" || input.SourceType == "" || input.Severity == "" || input.Severity == enum.SignalSeverityInfo {
		return entity.BlockingSignal{}, errs.ErrInvalidArgument
	}
	if err := validateReleaseSafeRef("blocking_signal.target_type", target.Type, true); err != nil {
		return entity.BlockingSignal{}, err
	}
	if err := validateReleaseSafeRef("blocking_signal.target_ref", target.Ref, true); err != nil {
		return entity.BlockingSignal{}, err
	}
	sourceRef := strings.TrimSpace(input.SourceRef)
	if err := validateReleaseSafeRef("blocking_signal.source_ref", sourceRef, false); err != nil {
		return entity.BlockingSignal{}, err
	}
	summary, err := normalizeReleaseSafeText("blocking_signal.summary", input.Summary, maxEvaluationFactorSummary)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSignalRecord, signalTargetResource(target)); err != nil {
		return entity.BlockingSignal{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationRecordBlockingSignal.String(), governanceevents.AggregateBlockingSignal)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	if replayed {
		signal, err := s.repository.GetBlockingSignal(ctx, result.AggregateID)
		if err != nil {
			return entity.BlockingSignal{}, err
		}
		if !sameExternalRef(signal.Target, input.Target) {
			return entity.BlockingSignal{}, errs.ErrConflict
		}
		return signal, nil
	}
	now := s.clock.Now()
	signal := entity.BlockingSignal{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Target:        target,
		SourceType:    input.SourceType,
		SourceRef:     sourceRef,
		Severity:      input.Severity,
		Summary:       summary,
		Status:        enum.BlockingSignalStatusActive,
	}
	result = commandResult(input.Meta, enum.OperationRecordBlockingSignal.String(), governanceevents.AggregateBlockingSignal, signal.ID, now)
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventBlockingSignalRecorded, governanceevents.AggregateBlockingSignal, signal.ID, now, governanceevents.Payload{
		BlockingSignalID: signal.ID.String(),
		Status:           string(signal.Status),
		ReasonCode:       string(signal.SourceType),
		Version:          signal.Version,
	})
	if err := s.repository.RecordBlockingSignal(ctx, signal, result, event); err != nil {
		return entity.BlockingSignal{}, err
	}
	return signal, nil
}

// ResolveBlockingSignal records a terminal resolution or dismissal.
func (s *Service) ResolveBlockingSignal(ctx context.Context, input ResolveBlockingSignalInput) (entity.BlockingSignal, error) {
	if input.BlockingSignalID == uuid.Nil || !terminalBlockingSignalStatus(input.TerminalStatus) {
		return entity.BlockingSignal{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSignalResolve, signalResource(input.BlockingSignalID)); err != nil {
		return entity.BlockingSignal{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationResolveBlockingSignal.String(), governanceevents.AggregateBlockingSignal)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	if replayed {
		if result.AggregateID != input.BlockingSignalID {
			return entity.BlockingSignal{}, errs.ErrConflict
		}
		return s.repository.GetBlockingSignal(ctx, result.AggregateID)
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	signal, err := s.repository.GetBlockingSignal(ctx, input.BlockingSignalID)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	if signal.Version != previousVersion {
		return entity.BlockingSignal{}, errs.ErrConflict
	}
	if signal.Status != enum.BlockingSignalStatusActive {
		return entity.BlockingSignal{}, errs.ErrPreconditionFailed
	}
	resolutionSummary, err := normalizeReleaseSafeText("blocking_signal.resolution_summary", input.ResolutionSummary, maxEvaluationFactorSummary)
	if err != nil {
		return entity.BlockingSignal{}, err
	}
	now := s.clock.Now()
	previousStatus := signal.Status
	signal.Status = input.TerminalStatus
	signal.Summary = resolutionSummary
	signal.Version = previousVersion + 1
	signal.UpdatedAt = now
	signal.ResolvedAt = &now
	result = commandResultWithPayload(input.Meta, enum.OperationResolveBlockingSignal.String(), governanceevents.AggregateBlockingSignal, signal.ID, now, map[string]any{
		"status": string(signal.Status),
	})
	event := outboxEvent(s.idGenerator.New(), governanceevents.EventBlockingSignalResolved, governanceevents.AggregateBlockingSignal, signal.ID, now, governanceevents.Payload{
		BlockingSignalID: signal.ID.String(),
		PreviousStatus:   string(previousStatus),
		Status:           string(signal.Status),
		ReasonCode:       blockingSignalReasonCode(signal.Status),
		Version:          signal.Version,
	})
	if err := s.repository.UpdateBlockingSignal(ctx, signal, previousVersion, result, event); err != nil {
		return entity.BlockingSignal{}, err
	}
	return signal, nil
}

func (s *Service) ListBlockingSignals(ctx context.Context, input ListBlockingSignalsInput) ([]entity.BlockingSignal, query.PageResult, error) {
	return listWithAuthorization(ctx, input.Meta, input.Filter, s.authorizeBlockingSignalList, s.repository.ListBlockingSignals)
}

// RecordReleaseSafetyState creates or updates current safety-loop state.
func (s *Service) RecordReleaseSafetyState(ctx context.Context, input RecordReleaseSafetyStateInput) (entity.ReleaseSafetyState, error) {
	if input.ReleaseDecisionPackageID == uuid.Nil || !validReleaseSafetyState(input.CurrentState) {
		return entity.ReleaseSafetyState{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionReleaseUpdate, releaseSafetyStateResource(input.ReleaseDecisionPackageID)); err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationRecordReleaseSafetyState.String(), governanceevents.AggregateReleaseSafetyState)
	if err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	if replayed {
		state, err := s.repository.GetReleaseSafetyStateByPackage(ctx, input.ReleaseDecisionPackageID)
		if err != nil {
			return entity.ReleaseSafetyState{}, err
		}
		if state.ID != result.AggregateID {
			return entity.ReleaseSafetyState{}, errs.ErrConflict
		}
		return state, nil
	}
	pkg, err := s.repository.GetReleaseDecisionPackage(ctx, input.ReleaseDecisionPackageID)
	if err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	now := s.clock.Now()
	existing, err := s.repository.GetReleaseSafetyStateByPackage(ctx, input.ReleaseDecisionPackageID)
	existingFound := err == nil
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.ReleaseSafetyState{}, err
	}
	activeSignals, _, err := s.repository.ListBlockingSignals(ctx, query.BlockingSignalFilter{
		Target: value.ExternalRef{Type: "release_candidate", Ref: pkg.ReleaseCandidateRef},
		Status: enum.BlockingSignalStatusActive,
		Page:   query.PageRequest{PageSize: 500},
	})
	if err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	runtimeJobRef := strings.TrimSpace(input.RuntimeJobRef)
	if err := validateReleaseSafeRef("release_safety.runtime_job_ref", runtimeJobRef, false); err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	lastStateReason, err := normalizeReleaseSafeText("release_safety.last_state_reason", input.LastStateReason, maxEvaluationFactorSummary)
	if err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	if !existingFound || existing.ID == uuid.Nil {
		state := entity.ReleaseSafetyState{
			VersionedBase:            entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ReleaseDecisionPackageID: input.ReleaseDecisionPackageID,
			CurrentState:             input.CurrentState,
			RuntimeJobRef:            runtimeJobRef,
			BlockingSignalCount:      int32(len(activeSignals)),
			LastStateReason:          lastStateReason,
		}
		result = commandResult(input.Meta, enum.OperationRecordReleaseSafetyState.String(), governanceevents.AggregateReleaseSafetyState, state.ID, now)
		event := releaseSafetyEvent(s.idGenerator.New(), now, state, pkg, releaseSafetyPreviousStatusNone, state.Version)
		if err := s.repository.RecordReleaseSafetyState(ctx, state, result, event); err != nil {
			return entity.ReleaseSafetyState{}, err
		}
		return state, nil
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	if existing.Version != previousVersion {
		return entity.ReleaseSafetyState{}, errs.ErrConflict
	}
	previousState := existing.CurrentState
	if err := ensureReleaseSafetyTransition(previousState, input.CurrentState); err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	existing.CurrentState = input.CurrentState
	existing.RuntimeJobRef = runtimeJobRef
	existing.BlockingSignalCount = int32(len(activeSignals))
	existing.LastStateReason = lastStateReason
	existing.Version = previousVersion + 1
	existing.UpdatedAt = now
	result = commandResult(input.Meta, enum.OperationRecordReleaseSafetyState.String(), governanceevents.AggregateReleaseSafetyState, existing.ID, now)
	event := releaseSafetyEvent(s.idGenerator.New(), now, existing, pkg, string(previousState), existing.Version)
	if err := s.repository.UpdateReleaseSafetyState(ctx, existing, previousVersion, result, event); err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	return existing, nil
}

func (s *Service) GetReleaseSafetyState(ctx context.Context, input GetReleaseSafetyStateInput) (entity.ReleaseSafetyState, error) {
	if input.ReleaseDecisionPackageID == uuid.Nil {
		return entity.ReleaseSafetyState{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeReleaseRead(ctx, input.Meta, input.ReleaseDecisionPackageID); err != nil {
		return entity.ReleaseSafetyState{}, err
	}
	return s.repository.GetReleaseSafetyStateByPackage(ctx, input.ReleaseDecisionPackageID)
}

func (s *Service) replayedReleaseDecision(ctx context.Context, id uuid.UUID) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error) {
	decision, err := s.repository.GetReleaseDecision(ctx, id)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	pkg, err := s.repository.GetReleaseDecisionPackage(ctx, decision.ReleaseDecisionPackageID)
	if err != nil {
		return entity.ReleaseDecision{}, entity.ReleaseDecisionPackage{}, err
	}
	return decision, pkg, nil
}

func (s *Service) ensureReleaseDecisionAllowed(ctx context.Context, pkg entity.ReleaseDecisionPackage, input SubmitReleaseDecisionInput) error {
	if !releaseOutcomeNeedsClearance(input.Outcome) {
		return nil
	}
	if pkg.RiskAssessmentID != nil {
		assessment, err := s.repository.GetRiskAssessment(ctx, *pkg.RiskAssessmentID)
		if err != nil {
			return err
		}
		if riskRequiresGate(assessment.EffectiveRiskClass) && input.GateDecisionID == nil {
			return errs.ErrPreconditionFailed
		}
	}
	if input.GateDecisionID != nil {
		gateDecision, err := s.repository.GetGateDecision(ctx, *input.GateDecisionID)
		if err != nil {
			return err
		}
		if !gateOutcomeClearsRelease(gateDecision.Outcome) {
			return errs.ErrPreconditionFailed
		}
	}
	signals, _, err := s.repository.ListBlockingSignals(ctx, query.BlockingSignalFilter{
		Target: value.ExternalRef{Type: "release_candidate", Ref: pkg.ReleaseCandidateRef},
		Status: enum.BlockingSignalStatusActive,
		Page:   query.PageRequest{PageSize: 1},
	})
	if err != nil {
		return err
	}
	if len(signals) > 0 {
		return errs.ErrPreconditionFailed
	}
	return nil
}

func releaseSafetyEvent(id uuid.UUID, now time.Time, state entity.ReleaseSafetyState, pkg entity.ReleaseDecisionPackage, previousStatus string, version int64) entity.OutboxEvent {
	previousStatus = strings.TrimSpace(previousStatus)
	if previousStatus == "" {
		previousStatus = releaseSafetyPreviousStatusNone
	}
	return outboxEvent(id, governanceevents.EventReleaseSafetyStateChanged, governanceevents.AggregateReleaseSafetyState, state.ID, now, governanceevents.Payload{
		ReleaseSafetyStateID:     state.ID.String(),
		ReleaseDecisionPackageID: pkg.ID.String(),
		ReleaseCandidateRef:      pkg.ReleaseCandidateRef,
		ProjectRef:               pkg.ProjectContext.ProjectRef,
		RuntimeJobRef:            state.RuntimeJobRef,
		PreviousStatus:           previousStatus,
		Status:                   string(state.CurrentState),
		ReasonCode:               releaseSafetyReasonCode(previousStatus, state.CurrentState),
		Version:                  version,
	})
}

func releaseSafetyReasonCode(previousStatus string, current enum.ReleaseSafetyStateKind) string {
	if previousStatus == releaseSafetyPreviousStatusNone {
		return "created"
	}
	return string(current)
}

func blockingSignalReasonCode(status enum.BlockingSignalStatus) string {
	switch status {
	case enum.BlockingSignalStatusResolved:
		return "resolved"
	case enum.BlockingSignalStatusDismissed:
		return "dismissed"
	default:
		return "unknown"
	}
}

func releaseOutcomeNeedsClearance(outcome enum.ReleaseDecisionOutcome) bool {
	return outcome == enum.ReleaseDecisionOutcomeGo || outcome == enum.ReleaseDecisionOutcomeGoWithConditions
}

func riskRequiresGate(risk enum.RiskClass) bool {
	return risk == enum.RiskClassR2 || risk == enum.RiskClassR3
}

func gateOutcomeClearsRelease(outcome enum.GateOutcome) bool {
	return outcome == enum.GateOutcomeApprove || outcome == enum.GateOutcomeApproveWithConditions
}

func terminalBlockingSignalStatus(status enum.BlockingSignalStatus) bool {
	return status == enum.BlockingSignalStatusResolved || status == enum.BlockingSignalStatusDismissed
}

func validReleaseSafetyState(state enum.ReleaseSafetyStateKind) bool {
	switch state {
	case enum.ReleaseSafetyStateKindReleaseCandidate,
		enum.ReleaseSafetyStateKindAwaitingReleaseGate,
		enum.ReleaseSafetyStateKindDeploying,
		enum.ReleaseSafetyStateKindPostdeployObservation,
		enum.ReleaseSafetyStateKindStable,
		enum.ReleaseSafetyStateKindHold,
		enum.ReleaseSafetyStateKindRollback,
		enum.ReleaseSafetyStateKindFollowUpRequired:
		return true
	default:
		return false
	}
}

func terminalReleaseSafetyState(state enum.ReleaseSafetyStateKind) bool {
	return state == enum.ReleaseSafetyStateKindStable ||
		state == enum.ReleaseSafetyStateKindRollback ||
		state == enum.ReleaseSafetyStateKindFollowUpRequired
}

func ensureReleaseSafetyTransition(previous enum.ReleaseSafetyStateKind, next enum.ReleaseSafetyStateKind) error {
	if !validReleaseSafetyState(previous) || !validReleaseSafetyState(next) {
		return errs.ErrInvalidArgument
	}
	if previous == next {
		return nil
	}
	if terminalReleaseSafetyState(previous) {
		return errs.ErrPreconditionFailed
	}
	if releaseSafetyTransitionAllowed(previous, next) {
		return nil
	}
	return errs.ErrPreconditionFailed
}

func releaseSafetyTransitionAllowed(previous enum.ReleaseSafetyStateKind, next enum.ReleaseSafetyStateKind) bool {
	switch previous {
	case enum.ReleaseSafetyStateKindReleaseCandidate:
		return isOneOfReleaseSafetyState(next,
			enum.ReleaseSafetyStateKindAwaitingReleaseGate,
			enum.ReleaseSafetyStateKindHold,
			enum.ReleaseSafetyStateKindRollback,
			enum.ReleaseSafetyStateKindFollowUpRequired,
		)
	case enum.ReleaseSafetyStateKindAwaitingReleaseGate:
		return isOneOfReleaseSafetyState(next,
			enum.ReleaseSafetyStateKindDeploying,
			enum.ReleaseSafetyStateKindHold,
			enum.ReleaseSafetyStateKindRollback,
			enum.ReleaseSafetyStateKindFollowUpRequired,
		)
	case enum.ReleaseSafetyStateKindDeploying:
		return isOneOfReleaseSafetyState(next,
			enum.ReleaseSafetyStateKindPostdeployObservation,
			enum.ReleaseSafetyStateKindHold,
			enum.ReleaseSafetyStateKindRollback,
			enum.ReleaseSafetyStateKindFollowUpRequired,
		)
	case enum.ReleaseSafetyStateKindPostdeployObservation:
		return isOneOfReleaseSafetyState(next,
			enum.ReleaseSafetyStateKindStable,
			enum.ReleaseSafetyStateKindHold,
			enum.ReleaseSafetyStateKindRollback,
			enum.ReleaseSafetyStateKindFollowUpRequired,
		)
	case enum.ReleaseSafetyStateKindHold:
		return isOneOfReleaseSafetyState(next,
			enum.ReleaseSafetyStateKindAwaitingReleaseGate,
			enum.ReleaseSafetyStateKindDeploying,
			enum.ReleaseSafetyStateKindRollback,
			enum.ReleaseSafetyStateKindFollowUpRequired,
		)
	default:
		return false
	}
}

func isOneOfReleaseSafetyState(value enum.ReleaseSafetyStateKind, allowed ...enum.ReleaseSafetyStateKind) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func normalizeReleaseProjectContext(input value.ProjectContextRef) (value.ProjectContextRef, error) {
	contextRef := value.ProjectContextRef{
		ProjectRef:       strings.TrimSpace(input.ProjectRef),
		RepositoryRef:    strings.TrimSpace(input.RepositoryRef),
		ServiceRef:       strings.TrimSpace(input.ServiceRef),
		BranchRulesRef:   strings.TrimSpace(input.BranchRulesRef),
		ReleasePolicyRef: strings.TrimSpace(input.ReleasePolicyRef),
		ReleaseLineRef:   strings.TrimSpace(input.ReleaseLineRef),
	}
	for _, ref := range []struct {
		name     string
		value    string
		required bool
	}{
		{name: "release.project_ref", value: contextRef.ProjectRef, required: true},
		{name: "release.repository_ref", value: contextRef.RepositoryRef},
		{name: "release.service_ref", value: contextRef.ServiceRef},
		{name: "release.branch_rules_ref", value: contextRef.BranchRulesRef},
		{name: "release.release_policy_ref", value: contextRef.ReleasePolicyRef},
		{name: "release.release_line_ref", value: contextRef.ReleaseLineRef},
	} {
		if err := validateReleaseSafeRef(ref.name, ref.value, ref.required); err != nil {
			return value.ProjectContextRef{}, err
		}
	}
	return contextRef, nil
}

func normalizeReleaseRepositoryRefs(refs []string) ([]string, error) {
	if len(refs) > maxReleasePackageRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		normalized := strings.TrimSpace(ref)
		if normalized == "" {
			continue
		}
		if err := validateReleaseSafeRef("release.repository_refs", normalized, true); err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeReleaseJSONArrayPayload(name string, payload []byte) ([]byte, error) {
	raw := strings.TrimSpace(string(payload))
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if len(raw) > maxReleasePackageJSONBytes || unsafeReleaseText(raw) {
		return nil, errs.ErrInvalidArgument
	}
	var decoded []any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	if len(decoded) > maxReleasePackageRefs {
		return nil, errs.ErrInvalidArgument
	}
	normalized, err := normalizeReleaseJSONValue(name, decoded)
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(normalized)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return result, nil
}

func normalizeReleaseJSONObjectPayload(name string, payload []byte) ([]byte, error) {
	raw := strings.TrimSpace(string(payload))
	if raw == "" || raw == "null" || raw == "{}" {
		return nil, nil
	}
	if len(raw) > maxReleasePackageJSONBytes || unsafeReleaseText(raw) {
		return nil, errs.ErrInvalidArgument
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	normalized, err := normalizeReleaseJSONValue(name, decoded)
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(normalized)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return result, nil
}

func normalizeReleaseJSONValue(name string, item any) (any, error) {
	switch value := item.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			normalizedKey := strings.TrimSpace(key)
			if err := validateReleaseSafeRef(name+".key", normalizedKey, true); err != nil {
				return nil, err
			}
			normalizedChild, err := normalizeReleaseJSONValue(name+"."+normalizedKey, child)
			if err != nil {
				return nil, err
			}
			result[normalizedKey] = normalizedChild
		}
		return result, nil
	case []any:
		if len(value) > maxReleasePackageRefs {
			return nil, errs.ErrInvalidArgument
		}
		result := make([]any, 0, len(value))
		for _, child := range value {
			normalizedChild, err := normalizeReleaseJSONValue(name, child)
			if err != nil {
				return nil, err
			}
			result = append(result, normalizedChild)
		}
		return result, nil
	case string:
		normalized := strings.TrimSpace(value)
		if strings.ContainsAny(normalized, "{}\n\r\t") {
			return nil, errs.ErrInvalidArgument
		}
		if err := validateReleaseSafeText(name, normalized, maxEvaluationRefLength); err != nil {
			return nil, err
		}
		return normalized, nil
	case nil, bool, float64:
		return value, nil
	default:
		return nil, errs.ErrInvalidArgument
	}
}

func normalizeReleaseSafeText(name string, value string, maxLength int) (string, error) {
	normalized := strings.TrimSpace(value)
	if err := validateReleaseSafeText(name, normalized, maxLength); err != nil {
		return "", err
	}
	return normalized, nil
}

func validateReleaseSafeRef(name string, value string, required bool) error {
	if err := validateSafeRef(name, value, required); err != nil {
		return err
	}
	if strings.TrimSpace(value) != "" && unsafeReleaseText(value) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateReleaseSafeText(name string, value string, maxLength int) error {
	if err := validateSafeText(name, value, maxLength); err != nil {
		return err
	}
	if unsafeReleaseText(value) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func unsafeReleaseText(value string) bool {
	if unsafeEvaluationText(value) {
		return true
	}
	normalized := strings.ToLower(value)
	for _, marker := range []string{
		"raw provider payload",
		"raw diff",
		"raw report",
		"stdout",
		"stderr",
		"transcript",
		"secret=",
		"authorization:",
		"bearer ",
		"kubeconfig",
		"personal data",
		"pii",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
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
	if strings.TrimSpace(target.Type) == "" || strings.TrimSpace(target.Ref) == "" {
		return errs.ErrInvalidArgument
	}
	return s.authorizeQuery(ctx, meta, actionGateRead, gateTargetResource(target))
}

func (s *Service) authorizeRiskAssessmentRead(ctx context.Context, meta QueryMeta, riskAssessmentID uuid.UUID) error {
	return s.authorizeQuery(ctx, meta, actionRiskRead, riskAssessmentResource(riskAssessmentID))
}

func (s *Service) authorizeRiskAssessmentList(ctx context.Context, meta QueryMeta, filter query.RiskAssessmentFilter) error {
	if externalRefProvided(filter.Target) {
		if strings.TrimSpace(filter.Target.Type) == "" || strings.TrimSpace(filter.Target.Ref) == "" {
			return errs.ErrInvalidArgument
		}
		return s.authorizeQuery(ctx, meta, actionRiskRead, riskTargetResource(filter.Target))
	}
	if resourceID := firstNonEmpty(filter.ProjectContext.ProjectRef, filter.ProjectContext.RepositoryRef); resourceID != "" {
		return s.authorizeQuery(ctx, meta, actionRiskRead, riskContextResource(resourceID))
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeGateRequestList(ctx context.Context, meta QueryMeta, filter query.GateRequestFilter) error {
	if externalRefProvided(filter.Target) {
		return s.authorizeGateTargetRead(ctx, meta, filter.Target)
	}
	if filter.RiskAssessmentID != nil {
		return s.authorizeRiskAssessmentRead(ctx, meta, *filter.RiskAssessmentID)
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeReviewSignalList(ctx context.Context, meta QueryMeta, filter query.ReviewSignalFilter) error {
	if filter.RiskAssessmentID != nil {
		return s.authorizeRiskAssessmentRead(ctx, meta, *filter.RiskAssessmentID)
	}
	if externalRefProvided(filter.Target) {
		if strings.TrimSpace(filter.Target.Type) == "" || strings.TrimSpace(filter.Target.Ref) == "" {
			return errs.ErrInvalidArgument
		}
		return s.authorizeQuery(ctx, meta, actionRiskRead, riskTargetResource(filter.Target))
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeGateDecisionList(ctx context.Context, meta QueryMeta, filter query.GateDecisionFilter) error {
	if filter.GateRequestID != nil {
		return s.authorizeGateRead(ctx, meta, *filter.GateRequestID)
	}
	if externalRefProvided(filter.Target) {
		return s.authorizeGateTargetRead(ctx, meta, filter.Target)
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeReleaseRead(ctx context.Context, meta QueryMeta, id uuid.UUID) error {
	return s.authorizeQuery(ctx, meta, actionReleaseRead, releaseDecisionResource(id))
}

func (s *Service) authorizeReleasePackageList(ctx context.Context, meta QueryMeta, filter query.ReleaseDecisionPackageFilter) error {
	if strings.TrimSpace(filter.ReleaseCandidateRef) != "" {
		return s.authorizeQuery(ctx, meta, actionReleaseRead, releaseDecisionContextResource(filter.ReleaseCandidateRef))
	}
	if strings.TrimSpace(filter.ProjectContext.ProjectRef) != "" {
		return s.authorizeQuery(ctx, meta, actionReleaseRead, releaseDecisionContextResource(filter.ProjectContext.ProjectRef))
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeReleaseDecisionList(ctx context.Context, meta QueryMeta, filter query.ReleaseDecisionFilter) error {
	if filter.ReleaseDecisionPackageID != nil {
		return s.authorizeReleaseRead(ctx, meta, *filter.ReleaseDecisionPackageID)
	}
	if strings.TrimSpace(filter.ProjectContext.ProjectRef) != "" {
		return s.authorizeQuery(ctx, meta, actionReleaseRead, releaseDecisionContextResource(filter.ProjectContext.ProjectRef))
	}
	return errs.ErrInvalidArgument
}

func (s *Service) authorizeBlockingSignalList(ctx context.Context, meta QueryMeta, filter query.BlockingSignalFilter) error {
	if !externalRefProvided(filter.Target) {
		return errs.ErrInvalidArgument
	}
	if strings.TrimSpace(filter.Target.Type) == "" || strings.TrimSpace(filter.Target.Ref) == "" {
		return errs.ErrInvalidArgument
	}
	return s.authorizeQuery(ctx, meta, actionSignalRead, signalTargetResource(filter.Target))
}

func readByID[T any](
	ctx context.Context,
	id uuid.UUID,
	meta QueryMeta,
	authorize func(context.Context, QueryMeta, uuid.UUID) error,
	get func(context.Context, uuid.UUID) (T, error),
) (T, error) {
	var zero T
	if id == uuid.Nil {
		return zero, errs.ErrInvalidArgument
	}
	if err := authorize(ctx, meta, id); err != nil {
		return zero, err
	}
	return get(ctx, id)
}

func externalRefProvided(ref value.ExternalRef) bool {
	return strings.TrimSpace(ref.Type) != "" || strings.TrimSpace(ref.Ref) != ""
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
