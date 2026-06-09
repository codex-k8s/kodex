package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	governanceSummaryPageSize = int32(100)

	governanceSummaryCodeBlocked = "blocked_decisions_present"
	governanceSummaryCodePending = "pending_decisions_present"
	governanceSummaryCodePartial = "partial_governance_refs"
	governanceSummaryCodeClear   = "clear"

	governanceSummaryNextActionNone                   = "none"
	governanceSummaryNextActionResolveBlockingSignal  = "resolve_blocking_signal"
	governanceSummaryNextActionReviewBlockingDecision = "review_blocking_decision"
	governanceSummaryNextActionRequestGate            = "request_governance_gate"
	governanceSummaryNextActionRecordGateDecision     = "record_gate_decision"
	governanceSummaryNextActionRecordReleaseDecision  = "record_release_decision"
	governanceSummaryNextActionReviewPendingDecision  = "review_pending_decision"
	governanceSummaryNextActionReviewPartialRefs      = "review_partial_refs"
)

type governanceSummarySeen struct {
	pending   map[string]struct{}
	completed map[string]struct{}
	evidence  map[string]struct{}
}

func newGovernanceSummarySeen() governanceSummarySeen {
	return governanceSummarySeen{
		pending:   make(map[string]struct{}),
		completed: make(map[string]struct{}),
		evidence:  make(map[string]struct{}),
	}
}

// GetGovernanceSummary returns a bounded read model for owner/staff UI.
func (s *Service) GetGovernanceSummary(ctx context.Context, input GetGovernanceSummaryInput) (entity.GovernanceSummary, error) {
	scope, err := normalizeGovernanceSummaryScope(input.Scope)
	if err != nil {
		return entity.GovernanceSummary{}, err
	}
	summary := entity.GovernanceSummary{Scope: scope}
	seen := newGovernanceSummarySeen()

	if scope.ReleaseDecisionPackageID != nil {
		pkg, err := s.GetReleaseDecisionPackage(ctx, GetReleaseDecisionPackageInput{ReleaseDecisionPackageID: *scope.ReleaseDecisionPackageID, Meta: input.Meta})
		if err != nil {
			return entity.GovernanceSummary{}, err
		}
		if err := s.appendReleasePackageSummary(ctx, input.Meta, &summary, &seen, pkg); err != nil {
			return entity.GovernanceSummary{}, err
		}
	}

	if !externalRefProvided(scope.Target) && releasePackageSummaryFilterProvided(scope) {
		packages, _, err := s.ListReleaseDecisionPackages(ctx, ListReleaseDecisionPackagesInput{
			Filter: query.ReleaseDecisionPackageFilter{
				ProjectContext:      scope.ProjectContext,
				ReleaseCandidateRef: summaryReleaseCandidateRef(scope),
				IntegrationRef:      scope.IntegrationRef,
				Page:                query.PageRequest{PageSize: governanceSummaryPageSize},
			},
			Meta: input.Meta,
		})
		if err != nil {
			return entity.GovernanceSummary{}, err
		}
		for _, pkg := range packages {
			if pkg.ID == uuid.Nil {
				continue
			}
			if err := s.appendReleasePackageSummary(ctx, input.Meta, &summary, &seen, pkg); err != nil {
				return entity.GovernanceSummary{}, err
			}
		}
	}

	if externalRefProvided(scope.Target) {
		if err := s.appendTargetGovernanceSummary(ctx, input.Meta, &summary, &seen, scope.Target, scope.ProjectContext); err != nil {
			return entity.GovernanceSummary{}, err
		}
	}
	if !externalRefProvided(scope.Target) && projectContextSummaryFilterProvided(scope.ProjectContext) {
		if err := s.appendProjectGovernanceSummary(ctx, input.Meta, &summary, &seen, scope.ProjectContext); err != nil {
			return entity.GovernanceSummary{}, err
		}
	}

	summary.Status = governanceSummaryStatus(summary)
	return summary, nil
}

func normalizeGovernanceSummaryScope(scope entity.GovernanceSummaryScope) (entity.GovernanceSummaryScope, error) {
	result := entity.GovernanceSummaryScope{
		Target: value.ExternalRef{
			Type: strings.TrimSpace(scope.Target.Type),
			Ref:  strings.TrimSpace(scope.Target.Ref),
		},
		ProjectContext: value.ProjectContextRef{
			ProjectRef:       strings.TrimSpace(scope.ProjectContext.ProjectRef),
			RepositoryRef:    strings.TrimSpace(scope.ProjectContext.RepositoryRef),
			ServiceRef:       strings.TrimSpace(scope.ProjectContext.ServiceRef),
			BranchRulesRef:   strings.TrimSpace(scope.ProjectContext.BranchRulesRef),
			ReleasePolicyRef: strings.TrimSpace(scope.ProjectContext.ReleasePolicyRef),
			ReleaseLineRef:   strings.TrimSpace(scope.ProjectContext.ReleaseLineRef),
		},
		ReleaseCandidateRef:      strings.TrimSpace(scope.ReleaseCandidateRef),
		ReleaseDecisionPackageID: scope.ReleaseDecisionPackageID,
	}
	if externalRefProvided(result.Target) && (result.Target.Type == "" || result.Target.Ref == "") {
		return entity.GovernanceSummaryScope{}, errs.ErrInvalidArgument
	}
	if releaseIntegrationRefProvided(scope.IntegrationRef) {
		refs, err := normalizeReleaseIntegrationRefs([]value.ReleaseIntegrationRef{scope.IntegrationRef})
		if err != nil {
			return entity.GovernanceSummaryScope{}, err
		}
		result.IntegrationRef = refs[0]
	}
	if governanceSummarySelectorCount(result) != 1 {
		return entity.GovernanceSummaryScope{}, errs.ErrInvalidArgument
	}
	return result, nil
}

func governanceSummarySelectorCount(scope entity.GovernanceSummaryScope) int {
	count := 0
	if externalRefProvided(scope.Target) {
		count++
	}
	if !externalRefProvided(scope.Target) && projectContextSummaryFilterProvided(scope.ProjectContext) {
		count++
	}
	if strings.TrimSpace(scope.ReleaseCandidateRef) != "" {
		count++
	}
	if scope.ReleaseDecisionPackageID != nil {
		count++
	}
	if releaseIntegrationRefProvided(scope.IntegrationRef) {
		count++
	}
	return count
}

func (s *Service) appendTargetGovernanceSummary(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, target value.ExternalRef, project value.ProjectContextRef) error {
	assessments, _, err := s.ListRiskAssessments(ctx, ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{Target: target, ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, assessment := range assessments {
		appendRiskAssessmentSummary(summary, seen, assessment)
	}

	reviewSignals, _, err := s.ListReviewSignals(ctx, ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{Target: target, ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, signal := range reviewSignals {
		appendReviewSignalSummary(summary, seen, signal)
	}

	gateRequests, _, err := s.ListGateRequests(ctx, ListGateRequestsInput{
		Filter: query.GateRequestFilter{Target: target, ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, request := range gateRequests {
		appendGateRequestSummary(summary, seen, request)
	}

	gateDecisions, _, err := s.ListGateDecisions(ctx, ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{Target: target, ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, decision := range gateDecisions {
		appendGateDecisionSummary(summary, seen, decision)
	}

	blockingSignals, _, err := s.ListBlockingSignals(ctx, ListBlockingSignalsInput{
		Filter: query.BlockingSignalFilter{Target: target, ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, signal := range blockingSignals {
		appendBlockingSignalSummary(summary, seen, signal)
	}
	return nil
}

func (s *Service) appendProjectGovernanceSummary(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, project value.ProjectContextRef) error {
	assessments, _, err := s.ListRiskAssessments(ctx, ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{ProjectContext: project, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, assessment := range assessments {
		appendRiskAssessmentSummary(summary, seen, assessment)
	}
	return nil
}

func (s *Service) appendReleasePackageSummary(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, pkg entity.ReleaseDecisionPackage) error {
	appendReleaseDecisionPackageSummary(summary, seen, pkg)
	appendReleasePackageEvidenceSummaries(summary, seen, pkg)

	decisions, _, err := s.repository.ListReleaseDecisions(ctx, query.ReleaseDecisionFilter{
		ReleaseDecisionPackageID: &pkg.ID,
		Page:                     query.PageRequest{PageSize: governanceSummaryPageSize},
	})
	if err != nil {
		return err
	}
	for _, decision := range decisions {
		if decision.ID == uuid.Nil {
			continue
		}
		appendReleaseDecisionSummary(summary, seen, decision)
	}

	state, err := s.repository.GetReleaseSafetyStateByPackage(ctx, pkg.ID)
	if err != nil {
		if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
	} else if state.ID != uuid.Nil {
		appendReleaseSafetyStateSummary(summary, seen, state)
	}

	if pkg.RiskAssessmentID != nil {
		assessment, err := s.GetRiskAssessment(ctx, GetRiskAssessmentInput{RiskAssessmentID: *pkg.RiskAssessmentID, Meta: meta})
		if err != nil {
			if !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			appendSummaryDiagnostic(summary, "missing_risk_assessment_ref")
		} else {
			appendRiskAssessmentSummary(summary, seen, assessment)
			if err := s.appendGateRequestsByRiskAssessmentID(ctx, meta, summary, seen, assessment.ID); err != nil {
				return err
			}
		}
	}
	for _, signalID := range pkg.ReviewSignalIDs {
		if err := s.appendReviewSignalByID(ctx, meta, summary, seen, signalID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) appendGateRequestsByRiskAssessmentID(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, assessmentID uuid.UUID) error {
	gateRequests, _, err := s.ListGateRequests(ctx, ListGateRequestsInput{
		Filter: query.GateRequestFilter{RiskAssessmentID: &assessmentID, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, request := range gateRequests {
		if request.ID == uuid.Nil {
			continue
		}
		appendGateRequestSummary(summary, seen, request)
		if err := s.appendGateDecisionsByGateRequestID(ctx, meta, summary, seen, request.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) appendGateDecisionsByGateRequestID(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, gateRequestID uuid.UUID) error {
	gateDecisions, _, err := s.ListGateDecisions(ctx, ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{GateRequestID: &gateRequestID, Page: query.PageRequest{PageSize: governanceSummaryPageSize}},
		Meta:   meta,
	})
	if err != nil {
		return err
	}
	for _, decision := range gateDecisions {
		if decision.ID == uuid.Nil {
			continue
		}
		appendGateDecisionSummary(summary, seen, decision)
	}
	return nil
}

func (s *Service) appendReviewSignalByID(ctx context.Context, meta QueryMeta, summary *entity.GovernanceSummary, seen *governanceSummarySeen, id uuid.UUID) error {
	signal, err := s.repository.GetReviewSignal(ctx, id)
	if err != nil {
		if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
		appendSummaryDiagnostic(summary, "missing_review_signal_ref")
		return nil
	}
	if signal.RiskAssessmentID != nil {
		if err := s.authorizeRiskAssessmentRead(ctx, meta, *signal.RiskAssessmentID); err != nil {
			return err
		}
	} else if externalRefProvided(signal.Target) {
		if err := s.authorizeReviewSignalList(ctx, meta, query.ReviewSignalFilter{Target: signal.Target}); err != nil {
			return err
		}
	} else {
		appendSummaryDiagnostic(summary, "review_signal_without_read_scope")
		return nil
	}
	appendReviewSignalSummary(summary, seen, signal)
	return nil
}

func appendRiskAssessmentSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, assessment entity.RiskAssessment) {
	item := entity.GovernanceDecisionSummary{
		Kind:              enum.GovernanceDecisionSummaryKindRiskAssessment,
		Attention:         riskAssessmentAttention(assessment),
		ID:                assessment.ID.String(),
		Target:            assessment.Target,
		ProjectContext:    assessment.ProjectContext,
		RiskClass:         assessment.EffectiveRiskClass,
		RequiredGateCount: int32(len(assessment.RequiredGates)),
		SafeSummary:       firstNonEmpty(assessment.Explanation, assessment.EvaluationSummary.Summary),
		EvidenceRefs:      assessment.EvidenceRefs,
		Version:           assessment.Version,
		CreatedAt:         assessment.CreatedAt,
		UpdatedAt:         assessment.UpdatedAt,
	}
	appendEvidenceRefSummaries(summary, seen, assessment.EvidenceRefs)
	appendSummaryDecision(summary, seen, item, riskAssessmentOpen(assessment))
}

func appendReviewSignalSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, signal entity.ReviewSignal) {
	item := signalDecisionSummaryBase(
		enum.GovernanceDecisionSummaryKindReviewSignal,
		reviewSignalAttention(signal.Outcome),
		signal.ID.String(),
		signal.Target,
		signal.Severity,
		signal.Summary,
		signal.CreatedAt,
		signal.CreatedAt,
	)
	item.ReviewOutcome = signal.Outcome
	item.EvidenceRefs = signal.EvidenceRefs
	if signal.RiskAssessmentID != nil {
		item.ParentID = signal.RiskAssessmentID.String()
	}
	appendEvidenceRefSummaries(summary, seen, signal.EvidenceRefs)
	appendSummaryDecision(summary, seen, item, reviewSignalNeedsAttention(signal.Outcome))
}

func appendGateRequestSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, request entity.GateRequest) {
	item := entity.GovernanceDecisionSummary{
		Kind:              enum.GovernanceDecisionSummaryKindGateRequest,
		Attention:         gateRequestAttention(request.Status),
		ID:                request.ID.String(),
		Target:            request.Target,
		GateRequestStatus: request.Status,
		SafeSummary:       firstNonEmpty(request.TerminalReason, request.EvidenceSummary),
		EvidenceRefs:      request.EvidenceRefs,
		Version:           request.Version,
		CreatedAt:         request.CreatedAt,
		UpdatedAt:         request.UpdatedAt,
	}
	if request.RiskAssessmentID != nil {
		item.ParentID = request.RiskAssessmentID.String()
	}
	appendEvidenceRefSummaries(summary, seen, request.EvidenceRefs)
	appendSummaryDecision(summary, seen, item, gateRequestOpen(request.Status))
}

func appendGateDecisionSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, decision entity.GateDecision) {
	item := entity.GovernanceDecisionSummary{
		Kind:        enum.GovernanceDecisionSummaryKindGateDecision,
		Attention:   gateOutcomeAttention(decision.Outcome),
		ID:          decision.ID.String(),
		ParentID:    decision.GateRequestID.String(),
		GateOutcome: decision.Outcome,
		SafeSummary: firstNonEmpty(decision.Reason, decision.ConditionsSummary),
		CreatedAt:   decision.DecidedAt,
		UpdatedAt:   decision.DecidedAt,
		ObservedAt:  formatSummaryTime(decision.DecidedAt),
	}
	appendSummaryDecision(summary, seen, item, false)
}

func appendReleaseDecisionPackageSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, pkg entity.ReleaseDecisionPackage) {
	item := entity.GovernanceDecisionSummary{
		Kind:                     enum.GovernanceDecisionSummaryKindReleaseDecisionPackage,
		Attention:                releasePackageAttention(pkg.Status),
		ID:                       pkg.ID.String(),
		ProjectContext:           pkg.ProjectContext,
		ReleaseCandidateRef:      pkg.ReleaseCandidateRef,
		ReleaseDecisionPackageID: pkg.ID.String(),
		ReleasePackageStatus:     pkg.Status,
		SafeSummary:              firstNonEmpty(pkg.KnownLimitationsSummary, pkg.ReleaseCandidateRef),
		EvidenceRefs:             pkg.EvidenceRefs,
		IntegrationRefs:          pkg.IntegrationRefs,
		ProviderRefs:             pkg.ProviderRefs,
		RuntimeRefs:              pkg.RuntimeRefs,
		AgentContext:             pkg.AgentContext,
		Version:                  pkg.Version,
		CreatedAt:                pkg.CreatedAt,
		UpdatedAt:                pkg.UpdatedAt,
	}
	if pkg.RiskAssessmentID != nil {
		item.ParentID = pkg.RiskAssessmentID.String()
	}
	appendSummaryDecision(summary, seen, item, pkg.Status != enum.ReleaseDecisionPackageStatusClosed)
}

func appendReleaseDecisionSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, decision entity.ReleaseDecision) {
	item := entity.GovernanceDecisionSummary{
		Kind:                     enum.GovernanceDecisionSummaryKindReleaseDecision,
		Attention:                releaseDecisionAttention(decision.Status, decision.Outcome),
		ID:                       decision.ID.String(),
		ParentID:                 decision.ReleaseDecisionPackageID.String(),
		ReleaseDecisionPackageID: decision.ReleaseDecisionPackageID.String(),
		ReleaseDecisionStatus:    decision.Status,
		ReleaseDecisionOutcome:   decision.Outcome,
		SafeSummary:              firstNonEmpty(decision.Reason, decision.ConditionsSummary),
		Version:                  decision.Version,
		CreatedAt:                decision.CreatedAt,
		UpdatedAt:                decision.UpdatedAt,
		ObservedAt:               formatSummaryTime(decision.DecidedAt),
	}
	if decision.GateDecisionID != nil {
		item.ParentID = decision.GateDecisionID.String()
	}
	appendSummaryDecision(summary, seen, item, decision.Status == enum.ReleaseDecisionStatusRequested)
}

func appendReleaseSafetyStateSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, state entity.ReleaseSafetyState) {
	item := entity.GovernanceDecisionSummary{
		Kind:                     enum.GovernanceDecisionSummaryKindReleaseSafetyState,
		Attention:                releaseSafetyAttention(state.CurrentState),
		ID:                       state.ID.String(),
		ParentID:                 state.ReleaseDecisionPackageID.String(),
		ReleaseDecisionPackageID: state.ReleaseDecisionPackageID.String(),
		SafeSummary:              state.LastStateReason,
		Version:                  state.Version,
		CreatedAt:                state.CreatedAt,
		UpdatedAt:                state.UpdatedAt,
	}
	appendSummaryDecision(summary, seen, item, releaseSafetyOpen(state.CurrentState))
}

func appendBlockingSignalSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, signal entity.BlockingSignal) {
	item := signalDecisionSummaryBase(
		enum.GovernanceDecisionSummaryKindBlockingSignal,
		blockingSignalAttention(signal.Status),
		signal.ID.String(),
		signal.Target,
		signal.Severity,
		signal.Summary,
		signal.CreatedAt,
		signal.UpdatedAt,
	)
	item.BlockingSignalStatus = signal.Status
	item.Version = signal.Version
	appendSummaryDecision(summary, seen, item, signal.Status == enum.BlockingSignalStatusActive)
}

func signalDecisionSummaryBase(kind enum.GovernanceDecisionSummaryKind, attention enum.GovernanceDecisionAttention, id string, target value.ExternalRef, severity enum.SignalSeverity, safeSummary string, createdAt time.Time, updatedAt time.Time) entity.GovernanceDecisionSummary {
	return entity.GovernanceDecisionSummary{
		Kind:        kind,
		Attention:   attention,
		ID:          id,
		Target:      target,
		Severity:    severity,
		SafeSummary: safeSummary,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func appendReleasePackageEvidenceSummaries(summary *entity.GovernanceSummary, seen *governanceSummarySeen, pkg entity.ReleaseDecisionPackage) {
	appendEvidenceRefSummaries(summary, seen, pkg.EvidenceRefs)
	for _, ref := range pkg.IntegrationRefs {
		appendEvidenceSummary(summary, seen, entity.GovernanceEvidenceSummary{
			SourceKind:      ref.Domain + "." + ref.Kind,
			SourceRef:       ref.Ref,
			Status:          ref.Status,
			SafeSummary:     ref.Summary,
			ErrorCode:       ref.ErrorCode,
			Digest:          ref.Digest,
			ObservedAt:      ref.ObservedAt,
			Version:         ref.Version,
			IntegrationRefs: []value.ReleaseIntegrationRef{ref},
		})
	}
}

func appendEvidenceRefSummaries(summary *entity.GovernanceSummary, seen *governanceSummarySeen, refs []value.EvidenceRef) {
	for _, ref := range refs {
		appendEvidenceSummary(summary, seen, entity.GovernanceEvidenceSummary{
			SourceKind:   ref.Kind,
			SourceRef:    ref.Ref,
			SafeSummary:  ref.Summary,
			Digest:       ref.Digest,
			EvidenceRefs: []value.EvidenceRef{ref},
		})
	}
}

func appendSummaryDecision(summary *entity.GovernanceSummary, seen *governanceSummarySeen, item entity.GovernanceDecisionSummary, pending bool) {
	if item.ID == "" || item.ID == uuid.Nil.String() {
		return
	}
	key := string(item.Kind) + "\x00" + item.ID
	if pending {
		if _, ok := seen.pending[key]; ok {
			return
		}
		seen.pending[key] = struct{}{}
		summary.PendingDecisions = append(summary.PendingDecisions, item)
		return
	}
	if _, ok := seen.completed[key]; ok {
		return
	}
	seen.completed[key] = struct{}{}
	summary.CompletedDecisions = append(summary.CompletedDecisions, item)
}

func appendEvidenceSummary(summary *entity.GovernanceSummary, seen *governanceSummarySeen, item entity.GovernanceEvidenceSummary) {
	if strings.TrimSpace(item.SourceKind) == "" || strings.TrimSpace(item.SourceRef) == "" {
		return
	}
	key := item.SourceKind + "\x00" + item.SourceRef
	if _, ok := seen.evidence[key]; ok {
		return
	}
	seen.evidence[key] = struct{}{}
	summary.EvidenceSummaries = append(summary.EvidenceSummaries, item)
}

func appendSummaryDiagnostic(summary *entity.GovernanceSummary, diagnostic string) {
	for _, existing := range summary.Diagnostics {
		if existing == diagnostic {
			return
		}
	}
	summary.Diagnostics = append(summary.Diagnostics, diagnostic)
}

func governanceSummaryStatus(summary entity.GovernanceSummary) entity.GovernanceSummaryStatus {
	status := entity.GovernanceSummaryStatus{
		Attention:              enum.GovernanceDecisionAttentionCompleted,
		PendingDecisionCount:   int32(len(summary.PendingDecisions)),
		CompletedDecisionCount: int32(len(summary.CompletedDecisions)),
		EvidenceCount:          int32(len(summary.EvidenceSummaries)),
		DiagnosticCount:        int32(len(summary.Diagnostics)),
		SummaryCode:            governanceSummaryCodeClear,
		NextActionCode:         governanceSummaryNextActionNone,
	}
	coveredRequiredGates := governanceSummaryGateRequestCoverage(summary)
	for _, item := range summary.PendingDecisions {
		applyGovernanceSummaryDecisionStatus(&status, item, true, coveredRequiredGates)
	}
	for _, item := range summary.CompletedDecisions {
		applyGovernanceSummaryDecisionStatus(&status, item, false, coveredRequiredGates)
	}
	if status.BlockedDecisionCount > 0 {
		status.Attention = enum.GovernanceDecisionAttentionBlocked
		status.SummaryCode = governanceSummaryCodeBlocked
		status.NextActionCode = governanceSummaryBlockedNextAction(status)
		return status
	}
	if status.PendingDecisionCount > 0 {
		status.Attention = enum.GovernanceDecisionAttentionPending
		status.SummaryCode = governanceSummaryCodePending
		status.NextActionCode = governanceSummaryPendingNextAction(status)
		return status
	}
	if status.DiagnosticCount > 0 {
		status.Attention = enum.GovernanceDecisionAttentionInformational
		status.SummaryCode = governanceSummaryCodePartial
		status.NextActionCode = governanceSummaryNextActionReviewPartialRefs
	}
	return status
}

func governanceSummaryGateRequestCoverage(summary entity.GovernanceSummary) map[string]int32 {
	covered := make(map[string]int32)
	for _, item := range summary.PendingDecisions {
		applyGovernanceGateRequestCoverage(covered, item)
	}
	for _, item := range summary.CompletedDecisions {
		applyGovernanceGateRequestCoverage(covered, item)
	}
	return covered
}

func applyGovernanceGateRequestCoverage(covered map[string]int32, item entity.GovernanceDecisionSummary) {
	if item.Kind != enum.GovernanceDecisionSummaryKindGateRequest || strings.TrimSpace(item.ParentID) == "" {
		return
	}
	covered[strings.TrimSpace(item.ParentID)]++
}

func applyGovernanceSummaryDecisionStatus(status *entity.GovernanceSummaryStatus, item entity.GovernanceDecisionSummary, pending bool, coveredRequiredGates map[string]int32) {
	if item.Attention == enum.GovernanceDecisionAttentionBlocked {
		status.BlockedDecisionCount++
	}
	if item.Kind == enum.GovernanceDecisionSummaryKindGateRequest && pending {
		status.PendingGateCount++
	}
	if item.Kind == enum.GovernanceDecisionSummaryKindRiskAssessment && pending && item.RequiredGateCount > 0 {
		status.PendingRequiredGateCount += uncoveredRequiredGateCount(item, coveredRequiredGates)
	}
	if item.Kind == enum.GovernanceDecisionSummaryKindReleaseDecision && pending {
		status.NextActionCode = governanceSummaryNextActionRecordReleaseDecision
	}
	if item.Kind == enum.GovernanceDecisionSummaryKindBlockingSignal && item.BlockingSignalStatus == enum.BlockingSignalStatusActive {
		status.ActiveBlockingSignalCount++
	}
	if item.RiskClass != "" && (status.MaxRiskClass == "" || riskClassRank(item.RiskClass) > riskClassRank(status.MaxRiskClass)) {
		status.MaxRiskClass = item.RiskClass
	}
}

func uncoveredRequiredGateCount(item entity.GovernanceDecisionSummary, coveredRequiredGates map[string]int32) int32 {
	covered := coveredRequiredGates[strings.TrimSpace(item.ID)]
	if covered >= item.RequiredGateCount {
		return 0
	}
	return item.RequiredGateCount - covered
}

func governanceSummaryBlockedNextAction(status entity.GovernanceSummaryStatus) string {
	if status.ActiveBlockingSignalCount > 0 {
		return governanceSummaryNextActionResolveBlockingSignal
	}
	return governanceSummaryNextActionReviewBlockingDecision
}

func governanceSummaryPendingNextAction(status entity.GovernanceSummaryStatus) string {
	if status.PendingGateCount > 0 {
		return governanceSummaryNextActionRecordGateDecision
	}
	if status.PendingRequiredGateCount > 0 {
		return governanceSummaryNextActionRequestGate
	}
	if status.NextActionCode != "" && status.NextActionCode != governanceSummaryNextActionNone {
		return status.NextActionCode
	}
	return governanceSummaryNextActionReviewPendingDecision
}

func riskAssessmentOpen(assessment entity.RiskAssessment) bool {
	if assessment.Status == enum.RiskAssessmentStatusClosed || assessment.Status == enum.RiskAssessmentStatusSuperseded {
		return false
	}
	return len(assessment.RequiredGates) > 0 || riskClassRank(assessment.EffectiveRiskClass) >= riskClassRank(enum.RiskClassR2)
}

func riskAssessmentAttention(assessment entity.RiskAssessment) enum.GovernanceDecisionAttention {
	if assessment.Status == enum.RiskAssessmentStatusClosed || assessment.Status == enum.RiskAssessmentStatusSuperseded {
		return enum.GovernanceDecisionAttentionCompleted
	}
	if riskClassRank(assessment.EffectiveRiskClass) >= riskClassRank(enum.RiskClassR3) {
		return enum.GovernanceDecisionAttentionBlocked
	}
	if len(assessment.RequiredGates) > 0 || riskClassRank(assessment.EffectiveRiskClass) >= riskClassRank(enum.RiskClassR2) {
		return enum.GovernanceDecisionAttentionPending
	}
	return enum.GovernanceDecisionAttentionInformational
}

func reviewSignalNeedsAttention(outcome enum.ReviewSignalOutcome) bool {
	switch outcome {
	case enum.ReviewSignalOutcomeBlock, enum.ReviewSignalOutcomeRequestChanges, enum.ReviewSignalOutcomeRaiseRisk:
		return true
	default:
		return false
	}
}

func reviewSignalAttention(outcome enum.ReviewSignalOutcome) enum.GovernanceDecisionAttention {
	if reviewSignalNeedsAttention(outcome) {
		return enum.GovernanceDecisionAttentionBlocked
	}
	if outcome == enum.ReviewSignalOutcomeInformational {
		return enum.GovernanceDecisionAttentionInformational
	}
	return enum.GovernanceDecisionAttentionCompleted
}

func gateRequestOpen(status enum.GateRequestStatus) bool {
	switch status {
	case enum.GateRequestStatusRequested, enum.GateRequestStatusDelivering, enum.GateRequestStatusAwaitingDecision:
		return true
	default:
		return false
	}
}

func gateRequestAttention(status enum.GateRequestStatus) enum.GovernanceDecisionAttention {
	if gateRequestOpen(status) {
		return enum.GovernanceDecisionAttentionPending
	}
	return enum.GovernanceDecisionAttentionCompleted
}

func gateOutcomeAttention(outcome enum.GateOutcome) enum.GovernanceDecisionAttention {
	switch outcome {
	case enum.GateOutcomeReject, enum.GateOutcomeHold, enum.GateOutcomeRollback, enum.GateOutcomeEscalate, enum.GateOutcomeRevise:
		return enum.GovernanceDecisionAttentionBlocked
	case enum.GateOutcomeApprove, enum.GateOutcomeApproveWithConditions:
		return enum.GovernanceDecisionAttentionCompleted
	default:
		return enum.GovernanceDecisionAttentionInformational
	}
}

func releasePackageAttention(status enum.ReleaseDecisionPackageStatus) enum.GovernanceDecisionAttention {
	if status == enum.ReleaseDecisionPackageStatusClosed {
		return enum.GovernanceDecisionAttentionCompleted
	}
	return enum.GovernanceDecisionAttentionPending
}

func releaseDecisionAttention(status enum.ReleaseDecisionStatus, outcome enum.ReleaseDecisionOutcome) enum.GovernanceDecisionAttention {
	if status == enum.ReleaseDecisionStatusRequested {
		return enum.GovernanceDecisionAttentionPending
	}
	switch outcome {
	case enum.ReleaseDecisionOutcomeNoGo, enum.ReleaseDecisionOutcomeHold, enum.ReleaseDecisionOutcomeRollback, enum.ReleaseDecisionOutcomeFollowUpRequired:
		return enum.GovernanceDecisionAttentionBlocked
	case enum.ReleaseDecisionOutcomeGo, enum.ReleaseDecisionOutcomeGoWithConditions:
		return enum.GovernanceDecisionAttentionCompleted
	default:
		return enum.GovernanceDecisionAttentionInformational
	}
}

func releaseSafetyOpen(state enum.ReleaseSafetyStateKind) bool {
	switch state {
	case enum.ReleaseSafetyStateKindStable:
		return false
	case enum.ReleaseSafetyStateKindReleaseCandidate:
		return false
	default:
		return true
	}
}

func releaseSafetyAttention(state enum.ReleaseSafetyStateKind) enum.GovernanceDecisionAttention {
	switch state {
	case enum.ReleaseSafetyStateKindHold, enum.ReleaseSafetyStateKindRollback, enum.ReleaseSafetyStateKindFollowUpRequired:
		return enum.GovernanceDecisionAttentionBlocked
	case enum.ReleaseSafetyStateKindStable:
		return enum.GovernanceDecisionAttentionCompleted
	case enum.ReleaseSafetyStateKindReleaseCandidate:
		return enum.GovernanceDecisionAttentionInformational
	default:
		return enum.GovernanceDecisionAttentionPending
	}
}

func blockingSignalAttention(status enum.BlockingSignalStatus) enum.GovernanceDecisionAttention {
	if status == enum.BlockingSignalStatusActive {
		return enum.GovernanceDecisionAttentionBlocked
	}
	return enum.GovernanceDecisionAttentionCompleted
}

func releasePackageSummaryFilterProvided(scope entity.GovernanceSummaryScope) bool {
	return summaryReleaseCandidateRef(scope) != "" ||
		strings.TrimSpace(scope.ProjectContext.ProjectRef) != "" ||
		releaseIntegrationRefProvided(scope.IntegrationRef)
}

func projectContextSummaryFilterProvided(context value.ProjectContextRef) bool {
	return strings.TrimSpace(context.ProjectRef) != "" || strings.TrimSpace(context.RepositoryRef) != ""
}

func summaryReleaseCandidateRef(scope entity.GovernanceSummaryScope) string {
	if strings.TrimSpace(scope.ReleaseCandidateRef) != "" {
		return strings.TrimSpace(scope.ReleaseCandidateRef)
	}
	if scope.Target.Type == "release_candidate" {
		return strings.TrimSpace(scope.Target.Ref)
	}
	return ""
}

func releaseIntegrationRefProvided(ref value.ReleaseIntegrationRef) bool {
	return strings.TrimSpace(ref.Domain) != "" || strings.TrimSpace(ref.Kind) != "" || strings.TrimSpace(ref.Ref) != ""
}

func releaseIntegrationRefResource(ref value.ReleaseIntegrationRef) string {
	return strings.TrimSpace(ref.Domain) + ":" + strings.TrimSpace(ref.Kind) + ":" + strings.TrimSpace(ref.Ref)
}

func formatSummaryTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
