package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	humanGateCodeLimit    = 128
	humanGateSummaryLimit = 1000
)

var humanGateInteractionRequestNamespace = uuid.MustParse("7e8e3c89-deda-4f2f-9c8d-8de44d7d4a3f")

const (
	humanGateOwnerRequestRefPrefix = "agent:human_gate/"
	humanGateSourceOwnerRefPrefix  = "agent:human_gate/"
	humanGateRiskClassLow          = "low"
)

type humanGateCommandPayload struct {
	HumanGateRequest entity.HumanGateRequest `json:"human_gate_request"`
	Decision         *humanGateDecision      `json:"decision,omitempty"`
}

type humanGateDecision struct {
	HumanGateRequestID string `json:"human_gate_request_id"`
	Status             string `json:"status"`
	Outcome            string `json:"outcome"`
	SafeSummary        string `json:"safe_summary,omitempty"`
	humanGateDecisionInteraction
	humanGateDecisionGovernance
}

type humanGateDecisionInteraction struct {
	InteractionRequestRef          string `json:"interaction_request_ref,omitempty"`
	InteractionResponseRef         string `json:"interaction_response_ref,omitempty"`
	InteractionResponseFingerprint string `json:"interaction_response_fingerprint,omitempty"`
	InteractionRequestVersion      int64  `json:"interaction_request_version,omitempty"`
}

type humanGateDecisionGovernance struct {
	GovernanceGateRequestRef     string `json:"governance_gate_request_ref,omitempty"`
	GovernanceDecisionRef        string `json:"governance_decision_ref,omitempty"`
	GovernanceRiskAssessmentRef  string `json:"governance_risk_assessment_ref,omitempty"`
	GovernanceReleasePackageRef  string `json:"governance_release_decision_package_ref,omitempty"`
	GovernanceReleaseDecisionRef string `json:"governance_release_decision_ref,omitempty"`
	GovernanceRiskProfileRef     string `json:"governance_risk_profile_ref,omitempty"`
	GovernanceGatePolicyRef      string `json:"governance_gate_policy_ref,omitempty"`
	GovernanceReleasePolicyRef   string `json:"governance_release_policy_ref,omitempty"`
}

func (s *Service) RequestHumanGate(ctx context.Context, input RequestHumanGateInput) (entity.HumanGateRequest, error) {
	if err := s.requireRepository(); err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.HumanGateRequest{}, err
	}
	idempotencyKey, err := humanGateIdempotencyKey(input.Meta, operationRequestHumanGate)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	gate, err := s.normalizeHumanGateRequest(ctx, session, input, idempotencyKey)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	verifyReplay := verifyEntityRequestReplay(gate, s.repository.GetHumanGateRequest, humanGateID, sameHumanGateRequest)
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRequestHumanGate, enum.CommandAggregateTypeHumanGate, humanGateFromPayload, verifyReplay); ok || err != nil {
		return replay, err
	}
	if isTerminalSessionStatus(session.Status) {
		return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	needsInteractionRequest := s.shouldRequestHumanGateInteraction(gate)
	gateID, err := s.nextHumanGateID(input.Meta, needsInteractionRequest)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	gate.ID = gateID
	if needsInteractionRequest {
		interactionRequest, err := s.requestHumanGateInteraction(ctx, input.Meta, session, gate)
		if err != nil {
			return entity.HumanGateRequest{}, err
		}
		gate.InteractionRequestRef = strings.TrimSpace(interactionRequest.InteractionRequestRef)
	}
	gate.Version = 1
	gate.CreatedAt = now
	gate.UpdatedAt = now
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate})
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	result, err := commandResult(input.Meta, operationRequestHumanGate, enum.CommandAggregateTypeHumanGate, gate.ID, payload, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	event, err := humanGateRequestedEvent(s.idGenerator.New(), gate, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return gate, s.repository.CreateHumanGateRequestWithResult(ctx, gate, result, event)
}

func (s *Service) shouldRequestHumanGateInteraction(gate entity.HumanGateRequest) bool {
	return s.humanGateRequestEnabled && strings.TrimSpace(gate.InteractionRequestRef) == ""
}

func (s *Service) nextHumanGateID(meta value.CommandMeta, deterministic bool) (uuid.UUID, error) {
	if !deterministic {
		return s.idGenerator.New(), nil
	}
	key, err := safeCommandResultKey(meta, operationRequestHumanGate, unsafeHumanGateText)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.NewSHA1(humanGateInteractionRequestNamespace, []byte(key)), nil
}

func (s *Service) requestHumanGateInteraction(ctx context.Context, meta value.CommandMeta, session entity.AgentSession, gate entity.HumanGateRequest) (HumanGateInteractionRequestResult, error) {
	input, err := humanGateInteractionRequestInput(meta, session, gate)
	if err != nil {
		return HumanGateInteractionRequestResult{}, err
	}
	result, err := s.humanGateRequester.RequestHumanGate(ctx, input)
	if err != nil {
		return HumanGateInteractionRequestResult{}, err
	}
	if strings.TrimSpace(result.InteractionRequestRef) == "" {
		return HumanGateInteractionRequestResult{}, errs.ErrDependencyUnavailable
	}
	return result, nil
}

func humanGateInteractionRequestInput(meta value.CommandMeta, session entity.AgentSession, gate entity.HumanGateRequest) (HumanGateInteractionRequestInput, error) {
	if err := validateHumanGateInteractionScope(session.Scope); err != nil {
		return HumanGateInteractionRequestInput{}, err
	}
	target, err := humanGateInteractionTarget(session.CreatedByActorRef)
	if err != nil {
		return HumanGateInteractionRequestInput{}, err
	}
	if strings.TrimSpace(gate.SafeSummary) == "" {
		return HumanGateInteractionRequestInput{}, errs.ErrInvalidArgument
	}
	return HumanGateInteractionRequestInput{
		Meta:                     humanGateInteractionMeta(meta, gate.ID),
		HumanGateRequestID:       gate.ID,
		Scope:                    session.Scope,
		SourceOwnerRef:           humanGateSourceOwnerRef(gate.ID),
		IngressRef:               humanGateInteractionIngressRef(meta),
		PromptSummary:            gate.SafeSummary,
		TargetRefs:               []HumanGateInteractionActorRef{target},
		ContextRefs:              humanGateInteractionContextRefs(session, gate),
		AllowedActions:           humanGateInteractionActions(),
		RiskClass:                humanGateRiskClassLow,
		GovernanceGateRequestRef: gate.GovernanceContext.GateRequestRef,
	}, nil
}

func validateHumanGateInteractionScope(scope value.ScopeRef) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	switch strings.TrimSpace(scope.Type) {
	case string(enum.AgentScopeTypePlatform),
		string(enum.AgentScopeTypeOrganization),
		string(enum.AgentScopeTypeProject),
		string(enum.AgentScopeTypeRepository):
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func humanGateInteractionMeta(meta value.CommandMeta, gateID uuid.UUID) value.CommandMeta {
	outgoing := meta
	outgoing.ExpectedVersion = nil
	if strings.TrimSpace(outgoing.IdempotencyKey) == "" {
		outgoing.IdempotencyKey = "agent_manager:human_gate_request:" + gateID.String()
	}
	return outgoing
}

func humanGateInteractionTarget(actorRef string) (HumanGateInteractionActorRef, error) {
	kind, ref, ok := strings.Cut(strings.TrimSpace(actorRef), ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(ref) == "" {
		return HumanGateInteractionActorRef{}, errs.ErrInvalidArgument
	}
	if unsafeHumanGateText(kind) || unsafeHumanGateText(ref) || len(kind) > followUpRefTextLimit || len(ref) > followUpRefTextLimit {
		return HumanGateInteractionActorRef{}, errs.ErrInvalidArgument
	}
	return HumanGateInteractionActorRef{Kind: strings.TrimSpace(kind), Ref: strings.TrimSpace(ref)}, nil
}

func humanGateSourceOwnerRef(gateID uuid.UUID) string {
	return humanGateSourceOwnerRefPrefix + gateID.String()
}

func humanGateOwnerRequestRef(gateID uuid.UUID) string {
	return humanGateOwnerRequestRefPrefix + gateID.String()
}

func humanGateInteractionIngressRef(meta value.CommandMeta) string {
	if meta.CommandID != uuid.Nil {
		return "agent-command:" + meta.CommandID.String()
	}
	idempotencyKey := strings.TrimSpace(meta.IdempotencyKey)
	if idempotencyKey == "" {
		return "agent-command:" + uuid.Nil.String()
	}
	return "agent-idempotency:" + shortSafeDigest(idempotencyKey)
}

func humanGateInteractionContextRefs(session entity.AgentSession, gate entity.HumanGateRequest) []HumanGateInteractionExternalRef {
	refs := []HumanGateInteractionExternalRef{
		{Kind: "agent_session", Ref: session.ID.String()},
		{Kind: "human_gate", Ref: humanGateOwnerRequestRef(gate.ID)},
	}
	if gate.RunID != nil {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "agent_run", Ref: gate.RunID.String()})
	}
	if gate.StageID != nil {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "agent_stage", Ref: gate.StageID.String()})
	}
	if gate.AcceptanceResultID != nil {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "acceptance_result", Ref: gate.AcceptanceResultID.String()})
	}
	if targetRef := strings.TrimSpace(gate.TargetRef); targetRef != "" {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "target", Ref: targetRef})
	}
	if providerRef := humanGateProviderContextRef(gate.ProviderTarget); providerRef != "" {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "provider_work_item", Ref: providerRef})
	}
	if governanceRef := strings.TrimSpace(gate.GovernanceGateRequestRef); governanceRef != "" {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "governance_gate_request", Ref: governanceRef})
	}
	if governanceRef := strings.TrimSpace(gate.GovernanceContext.RiskAssessmentRef); governanceRef != "" {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "governance_risk_assessment", Ref: governanceRef})
	}
	if governanceRef := strings.TrimSpace(gate.GovernanceContext.ReleaseDecisionPackageRef); governanceRef != "" {
		refs = append(refs, HumanGateInteractionExternalRef{Kind: "governance_release_decision_package", Ref: governanceRef})
	}
	return refs
}

func humanGateProviderContextRef(target value.ProviderTargetRef) string {
	if ref := strings.TrimSpace(target.WorkItemRef); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(target.PullRequestRef); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(target.CommentRef); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(target.ReviewSignalRef); ref != "" {
		return ref
	}
	return ""
}

func humanGateInteractionActions() []HumanGateInteractionAction {
	return []HumanGateInteractionAction{
		{ActionKey: string(enum.HumanGateOutcomeApprove), LabelTemplateRef: "interaction.actions.approve", Terminal: true},
		{ActionKey: string(enum.HumanGateOutcomeReject), LabelTemplateRef: "interaction.actions.reject", Terminal: true},
		{ActionKey: string(enum.HumanGateOutcomeRequestChanges), LabelTemplateRef: "interaction.actions.request_changes", Terminal: true},
		{ActionKey: string(enum.HumanGateOutcomeAnswer), LabelTemplateRef: "interaction.actions.answer", Terminal: true},
	}
}

func shortSafeDigest(value string) string {
	sum := sha256Sum(value)
	if len(sum) > 24 {
		return sum[:24]
	}
	return sum
}

func sha256Sum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) RecordHumanGateDecision(ctx context.Context, input RecordHumanGateDecisionInput) (entity.HumanGateRequest, error) {
	if err := s.requireRepository(); err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateID(input.HumanGateRequestID); err != nil {
		return entity.HumanGateRequest{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateHumanGateDecisionStatus(input.Status); err != nil {
		return entity.HumanGateRequest{}, err
	}
	outcome, err := normalizeHumanGateOutcome(input.Outcome)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	refs, err := normalizeHumanGateDecisionRefs(input)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	summary, err := normalizeHumanGateSummary(input.SafeSummary, false)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	fingerprint, err := normalizeHumanGateDecisionFingerprint(input.InteractionResponseFingerprint)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if input.InteractionRequestVersion < 0 {
		return entity.HumanGateRequest{}, errs.ErrInvalidArgument
	}
	decision := humanGateDecisionFromInput(input, outcome, refs, summary, fingerprint)
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordHumanGateDecision, enum.CommandAggregateTypeHumanGate, humanGateFromPayload, verifyHumanGateDecisionReplay(decision, s.repository.GetHumanGateRequest)); ok || err != nil {
		return replay, err
	}
	gate, err := s.repository.GetHumanGateRequest(ctx, input.HumanGateRequestID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if gate.Version != previousVersion {
		return entity.HumanGateRequest{}, errs.ErrConflict
	}
	if !humanGateAwaitingDecision(gate.Status) {
		return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
	}
	if err := validateHumanGateDecisionBinding(gate, refs); err != nil {
		return entity.HumanGateRequest{}, err
	}
	now := s.clock.Now()
	previousStatus := string(gate.Status)
	gate.Status = enum.HumanGateStatusResolved
	gate.Outcome = outcome
	gate.InteractionRequestRef = chooseString(refs.interactionRequestRef, gate.InteractionRequestRef)
	gate.InteractionResponseRef = chooseString(refs.interactionResponseRef, gate.InteractionResponseRef)
	gate.GovernanceGateRequestRef = chooseString(refs.governanceGateRequestRef, gate.GovernanceGateRequestRef)
	gate.GovernanceDecisionRef = chooseString(refs.governanceDecisionRef, gate.GovernanceDecisionRef)
	gate.GovernanceContext, err = mergeGovernanceContext(gate.GovernanceContext, refs.governanceContext)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	gate.SafeSummary = chooseString(summary, gate.SafeSummary)
	gate.ResolvedAt = &now
	gate.Version++
	gate.UpdatedAt = now
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate, Decision: &decision})
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	result, err := commandResult(input.Meta, operationRecordHumanGateDecision, enum.CommandAggregateTypeHumanGate, gate.ID, payload, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	event, err := humanGateResultEvent(s.idGenerator.New(), previousStatus, gate, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return gate, s.repository.UpdateHumanGateRequestWithResult(ctx, gate, previousVersion, result, event)
}

func (s *Service) GetHumanGateRequest(ctx context.Context, id uuid.UUID) (entity.HumanGateRequest, error) {
	return getByID(ctx, s, id, s.repository.GetHumanGateRequest)
}

func (s *Service) ListHumanGateRequests(ctx context.Context, filter query.HumanGateFilter) ([]entity.HumanGateRequest, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.repository.ListHumanGateRequests)
}

func (s *Service) normalizeHumanGateRequest(ctx context.Context, session entity.AgentSession, input RequestHumanGateInput, idempotencyKey string) (entity.HumanGateRequest, error) {
	runID := input.RunID
	stageID := input.StageID
	acceptanceID := input.AcceptanceResultID
	if acceptanceID != nil {
		acceptance, err := s.repository.GetAcceptanceResult(ctx, *acceptanceID)
		if err != nil {
			return entity.HumanGateRequest{}, err
		}
		if acceptance.SessionID != session.ID {
			return entity.HumanGateRequest{}, errs.ErrConflict
		}
		if acceptance.CheckKind != enum.AcceptanceCheckKindHumanGate || acceptance.Status != enum.AcceptanceStatusWaiting {
			return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&runID, acceptance.RunID); err != nil {
			return entity.HumanGateRequest{}, err
		}
		if err := bindOptionalUUID(&stageID, acceptance.StageID); err != nil {
			return entity.HumanGateRequest{}, err
		}
	}
	resolvedRunID, resolvedStageID, err := s.acceptanceRefs(ctx, session, runID, stageID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	providerTarget, err := normalizeFollowUpProviderTarget(input.ProviderTarget)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	targetRef, err := normalizeAcceptanceTargetRef(input.TargetRef)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	requestKind, err := normalizeHumanGateCode(input.RequestKind, true)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	reasonCode, err := normalizeHumanGateCode(input.ReasonCode, true)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	summary, err := normalizeHumanGateSummary(input.SafeSummary, false)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	interactionRequestRef, err := normalizeFollowUpOptionalRef(input.InteractionRequestRef)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	governanceContext, err := normalizeHumanGateRequestGovernanceContext(input)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return entity.HumanGateRequest{
		SessionID:                session.ID,
		RunID:                    resolvedRunID,
		StageID:                  resolvedStageID,
		AcceptanceResultID:       acceptanceID,
		ProviderTarget:           providerTarget,
		TargetRef:                targetRef,
		RequestKind:              requestKind,
		ReasonCode:               reasonCode,
		SafeSummary:              summary,
		InteractionRequestRef:    interactionRequestRef,
		GovernanceGateRequestRef: governanceContext.GateRequestRef,
		IdempotencyKey:           idempotencyKey,
		GovernanceContext:        governanceContext,
		Status:                   enum.HumanGateStatusWaiting,
		Outcome:                  enum.HumanGateOutcomeNone,
	}, nil
}

type humanGateDecisionRefs struct {
	interactionRequestRef    string
	interactionResponseRef   string
	governanceGateRequestRef string
	governanceDecisionRef    string
	governanceContext        value.GovernanceContextRef
}

func normalizeHumanGateDecisionRefs(input RecordHumanGateDecisionInput) (humanGateDecisionRefs, error) {
	interactionRequestRef, err := normalizeFollowUpOptionalRef(input.InteractionRequestRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	interactionResponseRef, err := normalizeFollowUpOptionalRef(input.InteractionResponseRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	governanceGateRequestRef, err := normalizeFollowUpOptionalRef(input.GovernanceGateRequestRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	governanceDecisionRef, err := normalizeFollowUpOptionalRef(input.GovernanceDecisionRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	governanceContext, err := normalizeHumanGateDecisionGovernanceContext(input, governanceGateRequestRef, governanceDecisionRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	if interactionResponseRef == "" && governanceContext.GateDecisionRef == "" {
		return humanGateDecisionRefs{}, errs.ErrInvalidArgument
	}
	return humanGateDecisionRefs{
		interactionRequestRef:    interactionRequestRef,
		interactionResponseRef:   interactionResponseRef,
		governanceGateRequestRef: governanceContext.GateRequestRef,
		governanceDecisionRef:    governanceContext.GateDecisionRef,
		governanceContext:        governanceContext,
	}, nil
}

func normalizeHumanGateRequestGovernanceContext(input RequestHumanGateInput) (value.GovernanceContextRef, error) {
	return governanceContextWithGateRefs(input.GovernanceContext, input.GovernanceGateRequestRef, "")
}

func normalizeHumanGateDecisionGovernanceContext(input RecordHumanGateDecisionInput, gateRequestRef string, gateDecisionRef string) (value.GovernanceContextRef, error) {
	return governanceContextWithGateRefs(input.GovernanceContext, gateRequestRef, gateDecisionRef)
}

func humanGateDecisionFromInput(input RecordHumanGateDecisionInput, outcome enum.HumanGateOutcome, refs humanGateDecisionRefs, summary string, fingerprint string) humanGateDecision {
	return humanGateDecision{
		HumanGateRequestID: input.HumanGateRequestID.String(),
		Status:             string(input.Status),
		Outcome:            string(outcome),
		SafeSummary:        summary,
		humanGateDecisionInteraction: humanGateDecisionInteraction{
			InteractionRequestRef:          refs.interactionRequestRef,
			InteractionResponseRef:         refs.interactionResponseRef,
			InteractionResponseFingerprint: fingerprint,
			InteractionRequestVersion:      input.InteractionRequestVersion,
		},
		humanGateDecisionGovernance: humanGateDecisionGovernance{
			GovernanceGateRequestRef:     refs.governanceGateRequestRef,
			GovernanceDecisionRef:        refs.governanceDecisionRef,
			GovernanceRiskAssessmentRef:  refs.governanceContext.RiskAssessmentRef,
			GovernanceReleasePackageRef:  refs.governanceContext.ReleaseDecisionPackageRef,
			GovernanceReleaseDecisionRef: refs.governanceContext.ReleaseDecisionRef,
			GovernanceRiskProfileRef:     refs.governanceContext.RiskProfileRef,
			GovernanceGatePolicyRef:      refs.governanceContext.GatePolicyRef,
			GovernanceReleasePolicyRef:   refs.governanceContext.ReleasePolicyRef,
		},
	}
}

func validateHumanGateDecisionBinding(gate entity.HumanGateRequest, refs humanGateDecisionRefs) error {
	if err := validateHumanGateStoredRef(gate.InteractionRequestRef, refs.interactionRequestRef); err != nil {
		return err
	}
	if err := validateHumanGateStoredRef(gate.GovernanceGateRequestRef, refs.governanceGateRequestRef); err != nil {
		return err
	}
	if _, err := mergeGovernanceContext(gate.GovernanceContext, refs.governanceContext); err != nil {
		return err
	}
	if refs.interactionResponseRef != "" && gate.InteractionRequestRef != "" && refs.interactionRequestRef == "" {
		return errs.ErrConflict
	}
	if refs.governanceDecisionRef != "" && gate.GovernanceGateRequestRef != "" && refs.governanceGateRequestRef == "" {
		return errs.ErrConflict
	}
	return nil
}

func validateHumanGateStoredRef(stored string, incoming string) error {
	if stored != "" && incoming != "" && stored != incoming {
		return errs.ErrConflict
	}
	return nil
}

func humanGateIdempotencyKey(meta value.CommandMeta, operation string) (string, error) {
	return safeCommandResultKey(meta, operation, unsafeHumanGateText)
}

func normalizeHumanGateCode(value string, required bool) (string, error) {
	normalized, err := normalizeSafeIdentifier(value, humanGateCodeLimit, unsafeHumanGateText)
	if err != nil {
		return "", err
	}
	if required && normalized == "" {
		return "", errs.ErrInvalidArgument
	}
	return normalized, nil
}

func normalizeHumanGateSummary(value string, required bool) (string, error) {
	return normalizeBoundedSafeText(value, humanGateSummaryLimit, required, unsafeHumanGateText)
}

func normalizeHumanGateDecisionFingerprint(value string) (string, error) {
	return normalizeSHA256Digest(value)
}

func normalizeHumanGateOutcome(outcome enum.HumanGateOutcome) (enum.HumanGateOutcome, error) {
	switch outcome {
	case enum.HumanGateOutcomeApprove,
		enum.HumanGateOutcomeReject,
		enum.HumanGateOutcomeRequestChanges,
		enum.HumanGateOutcomeAnswer:
		return outcome, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func validateHumanGateDecisionStatus(status enum.HumanGateStatus) error {
	if status != enum.HumanGateStatusResolved {
		return errs.ErrInvalidArgument
	}
	return nil
}

func humanGateAwaitingDecision(status enum.HumanGateStatus) bool {
	return status == enum.HumanGateStatusRequested || status == enum.HumanGateStatusWaiting
}

func unsafeHumanGateText(value string) bool {
	if unsafeFollowUpText(value) || unsafeAcceptanceTargetRef(value) {
		return true
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"email",
		"phone",
		"address",
		"pii",
		"interaction_payload",
		"governance_payload",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func humanGateFromPayload(payload []byte) (entity.HumanGateRequest, error) {
	var result humanGateCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.HumanGateRequest, err
}

func verifyHumanGateDecisionReplay(expected humanGateDecision, load func(context.Context, uuid.UUID) (entity.HumanGateRequest, error)) func(context.Context, entity.CommandResult, entity.HumanGateRequest) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.HumanGateRequest) error {
		if replay.ID != result.AggregateID || replay.ID.String() != expected.HumanGateRequestID {
			return errs.ErrConflict
		}
		if err := verifyHumanGateDecisionPayload(result.ResultPayload, expected); err != nil {
			return err
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if stored.ID != replay.ID || stored.Version != replay.Version || stored.Status != replay.Status || stored.Outcome != replay.Outcome {
			return errs.ErrConflict
		}
		return nil
	}
}

func verifyHumanGateDecisionPayload(payload []byte, expected humanGateDecision) error {
	var result humanGateCommandPayload
	if err := json.Unmarshal(payload, &result); err != nil {
		return err
	}
	if result.Decision == nil || !sameHumanGateDecision(*result.Decision, expected) {
		return errs.ErrConflict
	}
	return nil
}

func sameHumanGateDecision(left humanGateDecision, right humanGateDecision) bool {
	leftFields := humanGateDecisionFields(left)
	rightFields := humanGateDecisionFields(right)
	for index := range leftFields {
		if leftFields[index] != rightFields[index] {
			return false
		}
	}
	return true
}

func humanGateDecisionFields(decision humanGateDecision) [16]string {
	return [16]string{
		decision.HumanGateRequestID,
		decision.Status,
		decision.Outcome,
		decision.SafeSummary,
		decision.InteractionRequestRef,
		decision.InteractionResponseRef,
		decision.InteractionResponseFingerprint,
		strconv.FormatInt(decision.InteractionRequestVersion, 10),
		decision.GovernanceGateRequestRef,
		decision.GovernanceDecisionRef,
		decision.GovernanceRiskAssessmentRef,
		decision.GovernanceReleasePackageRef,
		decision.GovernanceReleaseDecisionRef,
		decision.GovernanceRiskProfileRef,
		decision.GovernanceGatePolicyRef,
		decision.GovernanceReleasePolicyRef,
	}
}

func sameHumanGateRequest(stored entity.HumanGateRequest, expected entity.HumanGateRequest) bool {
	return stored.SessionID == expected.SessionID &&
		sameUUIDPtr(stored.RunID, expected.RunID) &&
		sameUUIDPtr(stored.StageID, expected.StageID) &&
		sameUUIDPtr(stored.AcceptanceResultID, expected.AcceptanceResultID) &&
		stored.ProviderTarget == expected.ProviderTarget &&
		stored.TargetRef == expected.TargetRef &&
		stored.RequestKind == expected.RequestKind &&
		stored.ReasonCode == expected.ReasonCode &&
		stored.SafeSummary == expected.SafeSummary &&
		sameHumanGateInteractionRequestRef(stored.InteractionRequestRef, expected.InteractionRequestRef) &&
		stored.GovernanceGateRequestRef == expected.GovernanceGateRequestRef &&
		sameGovernanceContext(stored.GovernanceContext, expected.GovernanceContext) &&
		stored.IdempotencyKey == expected.IdempotencyKey
}

func sameHumanGateInteractionRequestRef(stored string, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	return strings.TrimSpace(stored) == expected
}

func humanGateID(gate entity.HumanGateRequest) uuid.UUID {
	return gate.ID
}

func sameUUIDPtr(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func chooseString(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
