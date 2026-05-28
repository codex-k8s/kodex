package grpc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

var protoJSON = protojson.MarshalOptions{UseProtoNames: true}

type protoEnum interface {
	~int32
	String() string
	Descriptor() protoreflect.EnumDescriptor
}

func protoEnumDomain[D ~string, E protoEnum](item E, prefix string, fallback D) D {
	return protoEnumDomainWith(item, prefix, fallback, strings.ToLower)
}

func protoEnumDomainWith[D ~string, E protoEnum](item E, prefix string, fallback D, normalize func(string) string) D {
	suffix, ok := strings.CutPrefix(item.String(), prefix)
	if !ok || suffix == "" || suffix == "UNSPECIFIED" {
		return fallback
	}
	return D(normalize(suffix))
}

func domainProtoEnum[D ~string, E protoEnum](item D, prefix string, unspecified E) E {
	value := strings.TrimSpace(string(item))
	if value == "" {
		return unspecified
	}
	descriptor := unspecified.Descriptor().Values().ByName(protoreflect.Name(prefix + strings.ToUpper(value)))
	if descriptor == nil {
		return unspecified
	}
	return E(descriptor.Number())
}

func commandMeta(meta *governancev1.CommandMeta) (governanceservice.CommandMeta, error) {
	if meta == nil {
		return governanceservice.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUID(meta.GetCommandId())
	if err != nil {
		return governanceservice.CommandMeta{}, err
	}
	var expectedVersion *int64
	if meta.ExpectedVersion != nil {
		value := meta.GetExpectedVersion()
		expectedVersion = &value
	}
	return governanceservice.CommandMeta{
		CommandID:       commandID,
		IdempotencyKey:  meta.GetIdempotencyKey(),
		ExpectedVersion: expectedVersion,
		Actor:           actor(meta.GetActor()),
		Reason:          meta.GetReason(),
		RequestID:       meta.GetRequestId(),
		RequestContext:  requestContext(meta.GetRequestContext()),
	}, nil
}

func queryMeta(meta *governancev1.QueryMeta) (governanceservice.QueryMeta, error) {
	if meta == nil {
		return governanceservice.QueryMeta{}, errs.ErrInvalidArgument
	}
	return governanceservice.QueryMeta{
		Actor:          actor(meta.GetActor()),
		RequestID:      meta.GetRequestId(),
		RequestContext: requestContext(meta.GetRequestContext()),
	}, nil
}

func actor(item *governancev1.Actor) value.Actor {
	if item == nil {
		return value.Actor{}
	}
	return value.Actor{Type: item.GetType(), ID: item.GetId()}
}

func requestContext(item *governancev1.RequestContext) value.RequestContext {
	if item == nil {
		return value.RequestContext{}
	}
	return value.RequestContext{
		Source:       item.GetSource(),
		TraceID:      item.GetTraceId(),
		SessionID:    item.GetSessionId(),
		ClientIPHash: item.GetClientIpHash(),
	}
}

func pageRequest(item *governancev1.PageRequest) query.PageRequest {
	if item == nil {
		return query.PageRequest{}
	}
	return query.PageRequest{PageSize: item.GetPageSize(), PageToken: item.GetPageToken()}
}

func pageResponse(item query.PageResult) *governancev1.PageResponse {
	return &governancev1.PageResponse{NextPageToken: ptrStringNonEmpty(item.NextPageToken)}
}

func scopeRef(item *governancev1.ScopeRef) value.ExternalRef {
	if item == nil {
		return value.ExternalRef{}
	}
	return value.ExternalRef{Type: scopeTypeString(item.GetType()), Ref: item.GetRef()}
}

func targetRef(item *governancev1.TargetRef) value.ExternalRef {
	if item == nil {
		return value.ExternalRef{}
	}
	return value.ExternalRef{Type: targetTypeString(item.GetType()), Ref: item.GetRef()}
}

func projectContext(item *governancev1.ProjectContextRef) value.ProjectContextRef {
	if item == nil {
		return value.ProjectContextRef{}
	}
	return value.ProjectContextRef{
		ProjectRef:       item.GetProjectRef(),
		RepositoryRef:    item.GetRepositoryRef(),
		ServiceRef:       item.GetServiceRef(),
		BranchRulesRef:   item.GetBranchRulesRef(),
		ReleasePolicyRef: item.GetReleasePolicyRef(),
		ReleaseLineRef:   item.GetReleaseLineRef(),
	}
}

func interactionDeliveryRef(item *governancev1.InteractionDeliveryRef) value.InteractionDeliveryRef {
	if item == nil {
		return value.InteractionDeliveryRef{}
	}
	result := value.InteractionDeliveryRef{}
	result.RequestRef = item.GetRequestRef()
	result.DeliveryRef = item.GetDeliveryRef()
	result.CallbackRef = item.GetCallbackRef()
	result.DecisionRef = item.GetDecisionRef()
	return result
}

func localizedTexts(items []*governancev1.LocalizedText) []value.LocalizedText {
	result := make([]value.LocalizedText, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, value.LocalizedText{Locale: item.GetLocale(), Text: item.GetText()})
	}
	return result
}

func evidenceRefs(items []*governancev1.EvidenceRef) []value.EvidenceRef {
	result := make([]value.EvidenceRef, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, value.EvidenceRef{
			Kind:           evidenceKindString(item.GetKind()),
			Ref:            item.GetRef(),
			Summary:        item.GetSummary(),
			Digest:         item.GetDigest(),
			RetentionClass: item.GetRetentionClass(),
		})
	}
	return result
}

func releaseIntegrationRefs(items []*governancev1.ReleaseIntegrationRef) []value.ReleaseIntegrationRef {
	result := make([]value.ReleaseIntegrationRef, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, value.ReleaseIntegrationRef{
			Domain:     item.GetDomain(),
			Kind:       item.GetKind(),
			Ref:        item.GetRef(),
			Status:     item.GetStatus(),
			Summary:    item.GetSummary(),
			Digest:     item.GetDigest(),
			ObservedAt: item.GetObservedAt(),
			Version:    item.GetVersion(),
			ErrorCode:  item.GetErrorCode(),
		})
	}
	return result
}

func riskEvaluationSummary(item *governancev1.RiskEvaluationSummary) value.RiskEvaluationSummary {
	if item == nil {
		return value.RiskEvaluationSummary{}
	}
	return value.RiskEvaluationSummary{
		ChangedFilesSummaryRef: item.GetChangedFilesSummaryRef(),
		Summary:                item.GetSummary(),
		Factors:                riskEvaluationFactors(item.GetFactors()),
	}
}

func riskEvaluationFactors(items []*governancev1.RiskEvaluationFactor) []value.RiskEvaluationFactor {
	result := make([]value.RiskEvaluationFactor, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, value.RiskEvaluationFactor{
			SourceType: string(riskFactorSourceType(item.GetSourceType())),
			Ref:        item.GetRef(),
			Summary:    item.GetSummary(),
			Tags:       item.GetTags(),
		})
	}
	return result
}

func riskRuleDrafts(profileID uuid.UUID, profileVersion int64, items []*governancev1.RiskRuleDraft) ([]entity.RiskRule, error) {
	result := make([]entity.RiskRule, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		requiredGatePolicyID, err := optionalUUID(item.GetRequiredGatePolicyId())
		if err != nil {
			return nil, err
		}
		result = append(result, entity.RiskRule{
			RiskProfileID:        profileID,
			ProfileVersion:       profileVersion,
			RuleKind:             riskRuleKind(item.GetRuleKind()),
			MatcherJSON:          []byte(strings.TrimSpace(item.GetMatcherJson())),
			MinRiskClass:         riskClass(item.GetMinRiskClass()),
			RequiredGatePolicyID: requiredGatePolicyID,
			ReasonTemplate:       localizedTexts(item.GetReasonTemplate()),
		})
	}
	return result, nil
}

func gatePolicyDrafts(profileID uuid.UUID, profileVersion int64, items []*governancev1.GatePolicyDraft) []entity.GatePolicy {
	result := make([]entity.GatePolicy, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		id := profileID
		result = append(result, entity.GatePolicy{
			RiskProfileID:          &id,
			ProfileVersion:         profileVersion,
			GateKind:               gateKind(item.GetGateKind()),
			MinRiskClass:           riskClass(item.GetMinRiskClass()),
			RequiredActorPolicyRef: item.GetRequiredActorPolicyRef(),
			RequiredSignalKinds:    item.GetRequiredSignalKinds(),
			TimeoutPolicyRef:       item.GetTimeoutPolicyRef(),
		})
	}
	return result
}

func protoObject(item proto.Message) ([]byte, error) {
	if item == nil {
		return nil, nil
	}
	payload, err := protoJSON.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal protobuf context: %v", errs.ErrInvalidArgument, err)
	}
	return payload, nil
}

func providerRefs(items []*governancev1.ProviderContextRef) ([]byte, error) {
	return protoArray(items)
}

func runtimeRefs(items []*governancev1.RuntimeContextRef) ([]byte, error) {
	return protoArray(items)
}

func protoArray[T proto.Message](items []T) ([]byte, error) {
	rawItems := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		payload, err := protoObject(item)
		if err != nil {
			return nil, err
		}
		if len(payload) == 0 {
			continue
		}
		rawItems = append(rawItems, json.RawMessage(payload))
	}
	payload, err := json.Marshal(rawItems)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal protobuf context array: %v", errs.ErrInvalidArgument, err)
	}
	return payload, nil
}

func reviewSignalIDs(values []string) ([]uuid.UUID, error) {
	var result []uuid.UUID
	for index := range values {
		id, err := requiredUUID(values[index])
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func toRiskProfile(profile entity.RiskProfile) *governancev1.RiskProfile {
	return &governancev1.RiskProfile{
		Id:            profile.ID.String(),
		Scope:         toScopeRef(profile.Scope),
		Slug:          profile.Slug,
		DisplayName:   toLocalizedTexts(profile.DisplayName),
		Description:   toLocalizedTexts(profile.Description),
		Status:        toRiskProfileStatus(profile.Status),
		ActiveVersion: profile.ActiveVersion,
		Version:       profile.Version,
		CreatedAt:     formatTime(profile.CreatedAt),
		UpdatedAt:     formatTime(profile.UpdatedAt),
	}
}

func toRiskProfileVersion(version entity.RiskProfileVersion) *governancev1.RiskProfileVersion {
	result := &governancev1.RiskProfileVersion{
		RiskProfileId:  version.RiskProfileID.String(),
		ProfileVersion: version.ProfileVersion,
		Status:         toRiskProfileVersionStatus(version.Status),
		Rules:          toRiskRules(version.Rules),
		GatePolicies:   toGatePolicies(version.GatePolicies),
		ContentDigest:  version.ContentDigest,
		CreatedAt:      formatTime(version.CreatedAt),
	}
	if version.ActivatedAt != nil {
		result.ActivatedAt = ptrString(formatTime(*version.ActivatedAt))
	}
	return result
}

func toRiskRules(items []entity.RiskRule) []*governancev1.RiskRule {
	result := make([]*governancev1.RiskRule, 0, len(items))
	for _, item := range items {
		rule := &governancev1.RiskRule{
			Id:             item.ID.String(),
			RiskProfileId:  item.RiskProfileID.String(),
			ProfileVersion: item.ProfileVersion,
			RuleKind:       toRiskRuleKind(item.RuleKind),
			MatcherJson:    string(item.MatcherJSON),
			MinRiskClass:   toRiskClass(item.MinRiskClass),
			ReasonTemplate: toLocalizedTexts(item.ReasonTemplate),
			Status:         toRuleStatus(item.Status),
			CreatedAt:      formatTime(item.CreatedAt),
			UpdatedAt:      formatTime(item.UpdatedAt),
		}
		if item.RequiredGatePolicyID != nil {
			rule.RequiredGatePolicyId = ptrString(item.RequiredGatePolicyID.String())
		}
		result = append(result, rule)
	}
	return result
}

func toGatePolicies(items []entity.GatePolicy) []*governancev1.GatePolicy {
	result := make([]*governancev1.GatePolicy, 0, len(items))
	for _, item := range items {
		policy := &governancev1.GatePolicy{
			Id:                     item.ID.String(),
			ProfileVersion:         item.ProfileVersion,
			GateKind:               toGateKind(item.GateKind),
			MinRiskClass:           toRiskClass(item.MinRiskClass),
			RequiredActorPolicyRef: item.RequiredActorPolicyRef,
			RequiredSignalKinds:    item.RequiredSignalKinds,
			TimeoutPolicyRef:       ptrStringNonEmpty(item.TimeoutPolicyRef),
			Status:                 toRuleStatus(item.Status),
		}
		if item.RiskProfileID != nil {
			policy.RiskProfileId = ptrString(item.RiskProfileID.String())
		}
		result = append(result, policy)
	}
	return result
}

func toRiskAssessment(item entity.RiskAssessment) *governancev1.RiskAssessment {
	result := &governancev1.RiskAssessment{
		Id:                 item.ID.String(),
		Target:             toTargetRef(item.Target),
		ProjectContext:     toProjectContext(item.ProjectContext),
		ProviderContext:    providerContextFromJSON(item.ProviderContext),
		AgentContext:       agentContextFromJSON(item.AgentContext),
		RuntimeContext:     runtimeContextFromJSON(item.RuntimeContext),
		InitialRiskClass:   toRiskClass(item.InitialRiskClass),
		EffectiveRiskClass: toRiskClass(item.EffectiveRiskClass),
		Status:             toRiskAssessmentStatus(item.Status),
		Explanation:        item.Explanation,
		RequiredGates:      toRequiredGates(item.RequiredGates),
		Version:            item.Version,
		EvaluationSummary:  toRiskEvaluationSummary(item.EvaluationSummary),
		EvidenceRefs:       toEvidenceRefs(item.EvidenceRefs),
		CreatedAt:          formatTime(item.CreatedAt),
		UpdatedAt:          formatTime(item.UpdatedAt),
	}
	if item.RiskProfileID != nil {
		result.RiskProfileId = ptrString(item.RiskProfileID.String())
	}
	if item.RiskProfileVersion != nil {
		result.RiskProfileVersion = item.RiskProfileVersion
	}
	return result
}

func toRiskFactors(items []entity.RiskFactor) []*governancev1.RiskFactor {
	result := make([]*governancev1.RiskFactor, 0, len(items))
	for _, item := range items {
		result = append(result, &governancev1.RiskFactor{
			Id:               item.ID.String(),
			RiskAssessmentId: item.RiskAssessmentID.String(),
			SourceType:       toRiskFactorSourceType(item.SourceType),
			SourceRef:        ptrStringNonEmpty(item.SourceRef),
			RiskClass:        toRiskClass(item.RiskClass),
			Summary:          item.Summary,
			CreatedAt:        formatTime(item.CreatedAt),
		})
	}
	return result
}

func toReviewSignal(item entity.ReviewSignal) *governancev1.ReviewSignal {
	result := &governancev1.ReviewSignal{
		Id:           item.ID.String(),
		Target:       toTargetRef(item.Target),
		RoleKind:     toReviewRoleKind(item.RoleKind),
		AuthorRef:    item.AuthorRef,
		Outcome:      toReviewSignalOutcome(item.Outcome),
		Severity:     toSignalSeverity(item.Severity),
		EvidenceRefs: toEvidenceRefs(item.EvidenceRefs),
		Summary:      item.Summary,
		CreatedAt:    formatTime(item.CreatedAt),
	}
	if item.RiskAssessmentID != nil {
		result.RiskAssessmentId = ptrString(item.RiskAssessmentID.String())
	}
	if item.Confidence != "" {
		confidence := toConfidence(item.Confidence)
		result.Confidence = &confidence
	}
	return result
}

func toReviewSignals(items []entity.ReviewSignal) []*governancev1.ReviewSignal {
	result := make([]*governancev1.ReviewSignal, 0, len(items))
	for _, item := range items {
		result = append(result, toReviewSignal(item))
	}
	return result
}

func toGateRequest(item entity.GateRequest) *governancev1.GateRequest {
	result := &governancev1.GateRequest{
		Id:                     item.ID.String(),
		Target:                 toTargetRef(item.Target),
		InteractionDeliveryRef: toInteractionDeliveryRef(item.InteractionDeliveryRef),
		EvidenceRefs:           toEvidenceRefs(item.EvidenceRefs),
		EvidenceSummary:        item.EvidenceSummary,
		Status:                 toGateRequestStatus(item.Status),
		Version:                item.Version,
		CreatedAt:              formatTime(item.CreatedAt),
		UpdatedAt:              formatTime(item.UpdatedAt),
		TerminalActorRef:       ptrStringNonEmpty(item.TerminalActorRef),
		TerminalReason:         ptrStringNonEmpty(item.TerminalReason),
	}
	if item.TerminalAt != nil {
		result.TerminalAt = ptrString(formatTime(*item.TerminalAt))
	}
	if item.RiskAssessmentID != nil {
		result.RiskAssessmentId = ptrString(item.RiskAssessmentID.String())
	}
	if item.GatePolicyID != nil {
		result.GatePolicyId = ptrString(item.GatePolicyID.String())
	}
	return result
}

func toGateRequests(items []entity.GateRequest) []*governancev1.GateRequest {
	result := make([]*governancev1.GateRequest, 0, len(items))
	for _, item := range items {
		result = append(result, toGateRequest(item))
	}
	return result
}

func toGateDecision(item entity.GateDecision) *governancev1.GateDecision {
	return &governancev1.GateDecision{
		Id:                item.ID.String(),
		GateRequestId:     item.GateRequestID.String(),
		DecisionActorRef:  item.DecisionActorRef,
		DecisionPolicyRef: item.DecisionPolicyRef,
		Outcome:           toGateOutcome(item.Outcome),
		Reason:            item.Reason,
		ConditionsSummary: ptrStringNonEmpty(item.ConditionsSummary),
		SourceRef:         ptrStringNonEmpty(item.SourceRef),
		DecidedAt:         formatTime(item.DecidedAt),
	}
}

func toGateDecisions(items []entity.GateDecision) []*governancev1.GateDecision {
	result := make([]*governancev1.GateDecision, 0, len(items))
	for _, item := range items {
		result = append(result, toGateDecision(item))
	}
	return result
}

func toReleaseDecisionPackage(item entity.ReleaseDecisionPackage) *governancev1.ReleaseDecisionPackage {
	result := &governancev1.ReleaseDecisionPackage{
		Id:                      item.ID.String(),
		ReleaseCandidateRef:     item.ReleaseCandidateRef,
		ProjectContext:          toProjectContext(item.ProjectContext),
		RepositoryRefs:          item.RepositoryRefs,
		ProviderRefs:            providerRefsFromJSON(item.ProviderRefs),
		RuntimeRefs:             runtimeRefsFromJSON(item.RuntimeRefs),
		AgentContext:            agentContextFromJSON(item.AgentContext),
		ReviewSignalIds:         uuidStrings(item.ReviewSignalIDs),
		EvidenceRefs:            toEvidenceRefs(item.EvidenceRefs),
		IntegrationRefs:         toReleaseIntegrationRefs(item.IntegrationRefs),
		KnownLimitationsSummary: item.KnownLimitationsSummary,
		Status:                  toReleaseDecisionPackageStatus(item.Status),
		Version:                 item.Version,
		CreatedAt:               formatTime(item.CreatedAt),
		UpdatedAt:               formatTime(item.UpdatedAt),
	}
	if item.RiskAssessmentID != nil {
		result.RiskAssessmentId = ptrString(item.RiskAssessmentID.String())
	}
	return result
}

func toReleaseIntegrationRefs(items []value.ReleaseIntegrationRef) []*governancev1.ReleaseIntegrationRef {
	result := make([]*governancev1.ReleaseIntegrationRef, 0, len(items))
	for _, item := range items {
		ref := &governancev1.ReleaseIntegrationRef{
			Domain: item.Domain,
			Kind:   item.Kind,
			Ref:    item.Ref,
		}
		ref.Status = ptrStringNonEmpty(item.Status)
		ref.Summary = ptrStringNonEmpty(item.Summary)
		ref.Digest = ptrStringNonEmpty(item.Digest)
		ref.ObservedAt = ptrStringNonEmpty(item.ObservedAt)
		ref.Version = ptrStringNonEmpty(item.Version)
		ref.ErrorCode = ptrStringNonEmpty(item.ErrorCode)
		result = append(result, ref)
	}
	return result
}

func toReleaseDecisionPackages(items []entity.ReleaseDecisionPackage) []*governancev1.ReleaseDecisionPackage {
	result := make([]*governancev1.ReleaseDecisionPackage, 0, len(items))
	for _, item := range items {
		result = append(result, toReleaseDecisionPackage(item))
	}
	return result
}

func toReleaseDecision(item entity.ReleaseDecision) *governancev1.ReleaseDecision {
	result := &governancev1.ReleaseDecision{
		Id:                       item.ID.String(),
		ReleaseDecisionPackageId: item.ReleaseDecisionPackageID.String(),
		Outcome:                  toReleaseDecisionOutcome(item.Outcome),
		DecisionActorRef:         item.DecisionActorRef,
		DecisionPolicyRef:        item.DecisionPolicyRef,
		Reason:                   item.Reason,
		ConditionsSummary:        ptrStringNonEmpty(item.ConditionsSummary),
		Status:                   toReleaseDecisionStatus(item.Status),
		Version:                  item.Version,
		DecidedAt:                formatTime(item.DecidedAt),
	}
	if item.GateDecisionID != nil {
		result.GateDecisionId = ptrString(item.GateDecisionID.String())
	}
	return result
}

func toReleaseDecisions(items []entity.ReleaseDecision) []*governancev1.ReleaseDecision {
	result := make([]*governancev1.ReleaseDecision, 0, len(items))
	for _, item := range items {
		result = append(result, toReleaseDecision(item))
	}
	return result
}

func toReleaseSafetyState(item entity.ReleaseSafetyState) *governancev1.ReleaseSafetyState {
	return &governancev1.ReleaseSafetyState{
		Id:                       item.ID.String(),
		ReleaseDecisionPackageId: item.ReleaseDecisionPackageID.String(),
		CurrentState:             toReleaseSafetyStateKind(item.CurrentState),
		RuntimeJobRef:            ptrStringNonEmpty(item.RuntimeJobRef),
		BlockingSignalCount:      item.BlockingSignalCount,
		LastStateReason:          item.LastStateReason,
		Version:                  item.Version,
		CreatedAt:                formatTime(item.CreatedAt),
		UpdatedAt:                formatTime(item.UpdatedAt),
	}
}

func toBlockingSignal(item entity.BlockingSignal) *governancev1.BlockingSignal {
	result := &governancev1.BlockingSignal{
		Id:         item.ID.String(),
		Target:     toTargetRef(item.Target),
		SourceType: toBlockingSignalSourceType(item.SourceType),
		SourceRef:  ptrStringNonEmpty(item.SourceRef),
		Severity:   toSignalSeverity(item.Severity),
		Summary:    item.Summary,
		Status:     toBlockingSignalStatus(item.Status),
		Version:    item.Version,
		CreatedAt:  formatTime(item.CreatedAt),
	}
	if item.ResolvedAt != nil {
		result.ResolvedAt = ptrString(formatTime(*item.ResolvedAt))
	}
	return result
}

func toBlockingSignals(items []entity.BlockingSignal) []*governancev1.BlockingSignal {
	result := make([]*governancev1.BlockingSignal, 0, len(items))
	for _, item := range items {
		result = append(result, toBlockingSignal(item))
	}
	return result
}

func toScopeRef(item value.ExternalRef) *governancev1.ScopeRef {
	return &governancev1.ScopeRef{Type: toScopeType(item.Type), Ref: item.Ref}
}

func toTargetRef(item value.ExternalRef) *governancev1.TargetRef {
	return &governancev1.TargetRef{Type: toTargetType(item.Type), Ref: item.Ref}
}

func toProjectContext(item value.ProjectContextRef) *governancev1.ProjectContextRef {
	return &governancev1.ProjectContextRef{
		ProjectRef:       ptrStringNonEmpty(item.ProjectRef),
		RepositoryRef:    ptrStringNonEmpty(item.RepositoryRef),
		ServiceRef:       ptrStringNonEmpty(item.ServiceRef),
		BranchRulesRef:   ptrStringNonEmpty(item.BranchRulesRef),
		ReleasePolicyRef: ptrStringNonEmpty(item.ReleasePolicyRef),
		ReleaseLineRef:   ptrStringNonEmpty(item.ReleaseLineRef),
	}
}

func toInteractionDeliveryRef(item value.InteractionDeliveryRef) *governancev1.InteractionDeliveryRef {
	return &governancev1.InteractionDeliveryRef{
		RequestRef:  ptrStringNonEmpty(item.RequestRef),
		DeliveryRef: ptrStringNonEmpty(item.DeliveryRef),
		CallbackRef: ptrStringNonEmpty(item.CallbackRef),
		DecisionRef: ptrStringNonEmpty(item.DecisionRef),
	}
}

func toLocalizedTexts(items []value.LocalizedText) []*governancev1.LocalizedText {
	result := make([]*governancev1.LocalizedText, 0, len(items))
	for _, item := range items {
		result = append(result, &governancev1.LocalizedText{Locale: item.Locale, Text: item.Text})
	}
	return result
}

func toEvidenceRefs(items []value.EvidenceRef) []*governancev1.EvidenceRef {
	result := make([]*governancev1.EvidenceRef, 0, len(items))
	for _, item := range items {
		result = append(result, &governancev1.EvidenceRef{
			Kind:           toEvidenceKind(item.Kind),
			Ref:            item.Ref,
			Summary:        item.Summary,
			Digest:         ptrStringNonEmpty(item.Digest),
			RetentionClass: ptrStringNonEmpty(item.RetentionClass),
		})
	}
	return result
}

func toRiskEvaluationSummary(item value.RiskEvaluationSummary) *governancev1.RiskEvaluationSummary {
	return &governancev1.RiskEvaluationSummary{
		ChangedFilesSummaryRef: ptrStringNonEmpty(item.ChangedFilesSummaryRef),
		Summary:                item.Summary,
		Factors:                toRiskEvaluationFactors(item.Factors),
	}
}

func toRiskEvaluationFactors(items []value.RiskEvaluationFactor) []*governancev1.RiskEvaluationFactor {
	result := make([]*governancev1.RiskEvaluationFactor, 0, len(items))
	for _, item := range items {
		result = append(result, &governancev1.RiskEvaluationFactor{
			SourceType: toRiskFactorSourceType(enum.RiskFactorSourceType(item.SourceType)),
			Ref:        item.Ref,
			Summary:    item.Summary,
			Tags:       item.Tags,
		})
	}
	return result
}

func toRequiredGates(items []entity.RequiredGate) []*governancev1.RequiredGate {
	result := make([]*governancev1.RequiredGate, 0, len(items))
	for _, item := range items {
		result = append(result, &governancev1.RequiredGate{
			GatePolicyId: item.GatePolicyID.String(),
			GateKind:     toGateKind(item.GateKind),
			MinRiskClass: toRiskClass(item.MinRiskClass),
			Reason:       item.Reason,
		})
	}
	return result
}

func providerContextFromJSON(payload []byte) *governancev1.ProviderContextRef {
	item := &governancev1.ProviderContextRef{}
	if unmarshalProtoJSON(payload, item) {
		return item
	}
	return nil
}

func agentContextFromJSON(payload []byte) *governancev1.AgentContextRef {
	item := &governancev1.AgentContextRef{}
	if unmarshalProtoJSON(payload, item) {
		return item
	}
	return nil
}

func runtimeContextFromJSON(payload []byte) *governancev1.RuntimeContextRef {
	item := &governancev1.RuntimeContextRef{}
	if unmarshalProtoJSON(payload, item) {
		return item
	}
	return nil
}

func providerRefsFromJSON(payload []byte) []*governancev1.ProviderContextRef {
	return protoRefsFromJSONArray(payload, func() *governancev1.ProviderContextRef { return &governancev1.ProviderContextRef{} })
}

func runtimeRefsFromJSON(payload []byte) []*governancev1.RuntimeContextRef {
	return protoRefsFromJSONArray(payload, func() *governancev1.RuntimeContextRef { return &governancev1.RuntimeContextRef{} })
}

func protoRefsFromJSONArray[T proto.Message](payload []byte, create func() T) []T {
	var raw []json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	result := make([]T, 0, len(raw))
	for _, body := range raw {
		item := create()
		if unmarshalProtoJSON(body, item) {
			result = append(result, item)
		}
	}
	return result
}

func unmarshalProtoJSON(payload []byte, item proto.Message) bool {
	if len(payload) == 0 || string(payload) == "{}" || string(payload) == "null" {
		return false
	}
	return protojson.Unmarshal(payload, item) == nil
}

func requiredUUID(value string) (uuid.UUID, error) {
	id, err := optionalUUID(value)
	if err != nil {
		return uuid.Nil, err
	}
	if id == nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return *id, nil
}

func optionalUUID(value string) (*uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid uuid %q", errs.ErrInvalidArgument, value)
	}
	return &id, nil
}

func ptrString(value string) *string {
	return &value
}

func ptrStringNonEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func uuidStrings(items []uuid.UUID) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.String())
	}
	return result
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func scopeTypeString(item governancev1.GovernanceScopeType) string {
	switch item {
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PLATFORM:
		return "platform"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_ORGANIZATION:
		return "organization"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PROJECT:
		return "project"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_REPOSITORY:
		return "repository"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_SERVICE:
		return "service"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PATH:
		return "path"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_API_ENDPOINT:
		return "api_endpoint"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_DATABASE_OBJECT:
		return "database_object"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_SECRET_AREA:
		return "secret_area"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RUNTIME_OPERATION:
		return "runtime_operation"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RELEASE_LINE:
		return "release_line"
	case governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RUNTIME_ENVIRONMENT:
		return "runtime_environment"
	default:
		return ""
	}
}

func toScopeType(item string) governancev1.GovernanceScopeType {
	switch item {
	case "platform":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PLATFORM
	case "organization":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_ORGANIZATION
	case "project":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PROJECT
	case "repository":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_REPOSITORY
	case "service":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_SERVICE
	case "path":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_PATH
	case "api_endpoint":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_API_ENDPOINT
	case "database_object":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_DATABASE_OBJECT
	case "secret_area":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_SECRET_AREA
	case "runtime_operation":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RUNTIME_OPERATION
	case "release_line":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RELEASE_LINE
	case "runtime_environment":
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_RUNTIME_ENVIRONMENT
	default:
		return governancev1.GovernanceScopeType_GOVERNANCE_SCOPE_TYPE_UNSPECIFIED
	}
}

func targetTypeString(item governancev1.GovernanceTargetType) string {
	switch item {
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION:
		return "transition"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST:
		return "pull_request"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE:
		return "release_candidate"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB:
		return "runtime_job"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE:
		return "policy_change"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT:
		return "document"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE:
		return "merge"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY:
		return "postdeploy"
	case governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK:
		return "rollback"
	default:
		return ""
	}
}

func toTargetType(item string) governancev1.GovernanceTargetType {
	switch item {
	case "transition":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION
	case "pull_request":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST
	case "release_candidate":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE
	case "runtime_job":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB
	case "policy_change":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE
	case "document":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT
	case "merge":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE
	case "postdeploy":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY
	case "rollback":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK
	default:
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_UNSPECIFIED
	}
}

func riskClass(item governancev1.RiskClass) enum.RiskClass {
	return protoEnumDomainWith(item, "RISK_CLASS_", enum.RiskClassUnspecified, strings.ToUpper)
}

func toRiskClass(item enum.RiskClass) governancev1.RiskClass {
	return domainProtoEnum(item, "RISK_CLASS_", governancev1.RiskClass_RISK_CLASS_UNSPECIFIED)
}

func riskProfileStatus(item governancev1.RiskProfileStatus) enum.RiskProfileStatus {
	return protoEnumDomain(item, "RISK_PROFILE_STATUS_", enum.RiskProfileStatus(""))
}

func toRiskProfileStatus(item enum.RiskProfileStatus) governancev1.RiskProfileStatus {
	return domainProtoEnum(item, "RISK_PROFILE_STATUS_", governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_UNSPECIFIED)
}

func toRiskProfileVersionStatus(item enum.RiskProfileVersionStatus) governancev1.RiskProfileVersionStatus {
	return domainProtoEnum(item, "RISK_PROFILE_VERSION_STATUS_", governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_UNSPECIFIED)
}

func riskRuleKind(item governancev1.RiskRuleKind) enum.RiskRuleKind {
	return protoEnumDomain(item, "RISK_RULE_KIND_", enum.RiskRuleKind(""))
}

func toRiskRuleKind(item enum.RiskRuleKind) governancev1.RiskRuleKind {
	return domainProtoEnum(item, "RISK_RULE_KIND_", governancev1.RiskRuleKind_RISK_RULE_KIND_UNSPECIFIED)
}

func ruleStatus(item governancev1.RuleStatus) enum.RuleStatus {
	return protoEnumDomain(item, "RULE_STATUS_", enum.RuleStatus(""))
}

func toRuleStatus(item enum.RuleStatus) governancev1.RuleStatus {
	return domainProtoEnum(item, "RULE_STATUS_", governancev1.RuleStatus_RULE_STATUS_UNSPECIFIED)
}

func gateKind(item governancev1.GateKind) enum.GateKind {
	return protoEnumDomain(item, "GATE_KIND_", enum.GateKind(""))
}

func toGateKind(item enum.GateKind) governancev1.GateKind {
	return domainProtoEnum(item, "GATE_KIND_", governancev1.GateKind_GATE_KIND_UNSPECIFIED)
}

func riskAssessmentStatus(item governancev1.RiskAssessmentStatus) enum.RiskAssessmentStatus {
	return protoEnumDomain(item, "RISK_ASSESSMENT_STATUS_", enum.RiskAssessmentStatus(""))
}

func toRiskAssessmentStatus(item enum.RiskAssessmentStatus) governancev1.RiskAssessmentStatus {
	return domainProtoEnum(item, "RISK_ASSESSMENT_STATUS_", governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_UNSPECIFIED)
}

func riskFactorSourceType(item governancev1.RiskFactorSourceType) enum.RiskFactorSourceType {
	return protoEnumDomain(item, "RISK_FACTOR_SOURCE_TYPE_", enum.RiskFactorSourceType(""))
}

func toRiskFactorSourceType(item enum.RiskFactorSourceType) governancev1.RiskFactorSourceType {
	return domainProtoEnum(item, "RISK_FACTOR_SOURCE_TYPE_", governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED)
}

func reviewRoleKind(item governancev1.ReviewRoleKind) enum.ReviewRoleKind {
	return protoEnumDomain(item, "REVIEW_ROLE_KIND_", enum.ReviewRoleKind(""))
}

func toReviewRoleKind(item enum.ReviewRoleKind) governancev1.ReviewRoleKind {
	return domainProtoEnum(item, "REVIEW_ROLE_KIND_", governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_UNSPECIFIED)
}

func reviewSignalOutcome(item governancev1.ReviewSignalOutcome) enum.ReviewSignalOutcome {
	return protoEnumDomain(item, "REVIEW_SIGNAL_OUTCOME_", enum.ReviewSignalOutcome(""))
}

func toReviewSignalOutcome(item enum.ReviewSignalOutcome) governancev1.ReviewSignalOutcome {
	return domainProtoEnum(item, "REVIEW_SIGNAL_OUTCOME_", governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_UNSPECIFIED)
}

func signalSeverity(item governancev1.SignalSeverity) enum.SignalSeverity {
	return protoEnumDomain(item, "SIGNAL_SEVERITY_", enum.SignalSeverity(""))
}

func toSignalSeverity(item enum.SignalSeverity) governancev1.SignalSeverity {
	return domainProtoEnum(item, "SIGNAL_SEVERITY_", governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED)
}

func confidence(item governancev1.Confidence) enum.Confidence {
	return protoEnumDomain(item, "CONFIDENCE_", enum.Confidence(""))
}

func toConfidence(item enum.Confidence) governancev1.Confidence {
	return domainProtoEnum(item, "CONFIDENCE_", governancev1.Confidence_CONFIDENCE_UNSPECIFIED)
}

func evidenceKindString(item governancev1.EvidenceKind) string {
	return protoEnumDomain(item, "EVIDENCE_KIND_", "")
}

func toEvidenceKind(item string) governancev1.EvidenceKind {
	return domainProtoEnum(item, "EVIDENCE_KIND_", governancev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED)
}

func gateRequestStatus(item governancev1.GateRequestStatus) enum.GateRequestStatus {
	return protoEnumDomain(item, "GATE_REQUEST_STATUS_", enum.GateRequestStatus(""))
}

func toGateRequestStatus(item enum.GateRequestStatus) governancev1.GateRequestStatus {
	return domainProtoEnum(item, "GATE_REQUEST_STATUS_", governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED)
}

func gateOutcome(item governancev1.GateOutcome) enum.GateOutcome {
	return protoEnumDomain(item, "GATE_OUTCOME_", enum.GateOutcome(""))
}

func toGateOutcome(item enum.GateOutcome) governancev1.GateOutcome {
	return domainProtoEnum(item, "GATE_OUTCOME_", governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED)
}

func releaseDecisionPackageStatus(item governancev1.ReleaseDecisionPackageStatus) enum.ReleaseDecisionPackageStatus {
	return protoEnumDomain(item, "RELEASE_DECISION_PACKAGE_STATUS_", enum.ReleaseDecisionPackageStatus(""))
}

func toReleaseDecisionPackageStatus(item enum.ReleaseDecisionPackageStatus) governancev1.ReleaseDecisionPackageStatus {
	return domainProtoEnum(item, "RELEASE_DECISION_PACKAGE_STATUS_", governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_UNSPECIFIED)
}

func releaseDecisionStatus(item governancev1.ReleaseDecisionStatus) enum.ReleaseDecisionStatus {
	return protoEnumDomain(item, "RELEASE_DECISION_STATUS_", enum.ReleaseDecisionStatus(""))
}

func toReleaseDecisionStatus(item enum.ReleaseDecisionStatus) governancev1.ReleaseDecisionStatus {
	return domainProtoEnum(item, "RELEASE_DECISION_STATUS_", governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_UNSPECIFIED)
}

func releaseDecisionOutcome(item governancev1.ReleaseDecisionOutcome) enum.ReleaseDecisionOutcome {
	return protoEnumDomain(item, "RELEASE_DECISION_OUTCOME_", enum.ReleaseDecisionOutcome(""))
}

func toReleaseDecisionOutcome(item enum.ReleaseDecisionOutcome) governancev1.ReleaseDecisionOutcome {
	return domainProtoEnum(item, "RELEASE_DECISION_OUTCOME_", governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_UNSPECIFIED)
}

func releaseSafetyStateKind(item governancev1.ReleaseSafetyStateKind) enum.ReleaseSafetyStateKind {
	return protoEnumDomain(item, "RELEASE_SAFETY_STATE_KIND_", enum.ReleaseSafetyStateKind(""))
}

func toReleaseSafetyStateKind(item enum.ReleaseSafetyStateKind) governancev1.ReleaseSafetyStateKind {
	return domainProtoEnum(item, "RELEASE_SAFETY_STATE_KIND_", governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_UNSPECIFIED)
}

func blockingSignalSourceType(item governancev1.BlockingSignalSourceType) enum.BlockingSignalSourceType {
	return protoEnumDomain(item, "BLOCKING_SIGNAL_SOURCE_TYPE_", enum.BlockingSignalSourceType(""))
}

func toBlockingSignalSourceType(item enum.BlockingSignalSourceType) governancev1.BlockingSignalSourceType {
	return domainProtoEnum(item, "BLOCKING_SIGNAL_SOURCE_TYPE_", governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_UNSPECIFIED)
}

func blockingSignalStatus(item governancev1.BlockingSignalStatus) enum.BlockingSignalStatus {
	return protoEnumDomain(item, "BLOCKING_SIGNAL_STATUS_", enum.BlockingSignalStatus(""))
}

func toBlockingSignalStatus(item enum.BlockingSignalStatus) governancev1.BlockingSignalStatus {
	return domainProtoEnum(item, "BLOCKING_SIGNAL_STATUS_", governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_UNSPECIFIED)
}
