package grpc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

var protoJSON = protojson.MarshalOptions{UseProtoNames: true}

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
	return value.InteractionDeliveryRef{
		RequestRef:  item.GetRequestRef(),
		DeliveryRef: item.GetDeliveryRef(),
		CallbackRef: item.GetCallbackRef(),
		DecisionRef: item.GetDecisionRef(),
	}
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
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := requiredUUID(value)
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

func toReleaseDecisionPackages(items []entity.ReleaseDecisionPackage) []*governancev1.ReleaseDecisionPackage {
	result := make([]*governancev1.ReleaseDecisionPackage, 0, len(items))
	for _, item := range items {
		result = append(result, toReleaseDecisionPackage(item))
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
	var raw []json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	result := make([]*governancev1.ProviderContextRef, 0, len(raw))
	for _, body := range raw {
		item := &governancev1.ProviderContextRef{}
		if unmarshalProtoJSON(body, item) {
			result = append(result, item)
		}
	}
	return result
}

func runtimeRefsFromJSON(payload []byte) []*governancev1.RuntimeContextRef {
	var raw []json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	result := make([]*governancev1.RuntimeContextRef, 0, len(raw))
	for _, body := range raw {
		item := &governancev1.RuntimeContextRef{}
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
	switch item {
	case governancev1.RiskClass_RISK_CLASS_R0:
		return enum.RiskClassR0
	case governancev1.RiskClass_RISK_CLASS_R1:
		return enum.RiskClassR1
	case governancev1.RiskClass_RISK_CLASS_R2:
		return enum.RiskClassR2
	case governancev1.RiskClass_RISK_CLASS_R3:
		return enum.RiskClassR3
	default:
		return enum.RiskClassUnspecified
	}
}

func toRiskClass(item enum.RiskClass) governancev1.RiskClass {
	switch item {
	case enum.RiskClassR0:
		return governancev1.RiskClass_RISK_CLASS_R0
	case enum.RiskClassR1:
		return governancev1.RiskClass_RISK_CLASS_R1
	case enum.RiskClassR2:
		return governancev1.RiskClass_RISK_CLASS_R2
	case enum.RiskClassR3:
		return governancev1.RiskClass_RISK_CLASS_R3
	default:
		return governancev1.RiskClass_RISK_CLASS_UNSPECIFIED
	}
}

func riskProfileStatus(item governancev1.RiskProfileStatus) enum.RiskProfileStatus {
	switch item {
	case governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_DRAFT:
		return enum.RiskProfileStatusDraft
	case governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_ACTIVE:
		return enum.RiskProfileStatusActive
	case governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_DISABLED:
		return enum.RiskProfileStatusDisabled
	case governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_ARCHIVED:
		return enum.RiskProfileStatusArchived
	default:
		return ""
	}
}

func toRiskProfileStatus(item enum.RiskProfileStatus) governancev1.RiskProfileStatus {
	switch item {
	case enum.RiskProfileStatusDraft:
		return governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_DRAFT
	case enum.RiskProfileStatusActive:
		return governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_ACTIVE
	case enum.RiskProfileStatusDisabled:
		return governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_DISABLED
	case enum.RiskProfileStatusArchived:
		return governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_ARCHIVED
	default:
		return governancev1.RiskProfileStatus_RISK_PROFILE_STATUS_UNSPECIFIED
	}
}

func toRiskProfileVersionStatus(item enum.RiskProfileVersionStatus) governancev1.RiskProfileVersionStatus {
	switch item {
	case enum.RiskProfileVersionStatusDraft:
		return governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_DRAFT
	case enum.RiskProfileVersionStatusActive:
		return governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_ACTIVE
	case enum.RiskProfileVersionStatusSuperseded:
		return governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_SUPERSEDED
	case enum.RiskProfileVersionStatusArchived:
		return governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_ARCHIVED
	default:
		return governancev1.RiskProfileVersionStatus_RISK_PROFILE_VERSION_STATUS_UNSPECIFIED
	}
}

func riskRuleKind(item governancev1.RiskRuleKind) enum.RiskRuleKind {
	switch item {
	case governancev1.RiskRuleKind_RISK_RULE_KIND_PATH:
		return enum.RiskRuleKindPath
	case governancev1.RiskRuleKind_RISK_RULE_KIND_SERVICE:
		return enum.RiskRuleKindService
	case governancev1.RiskRuleKind_RISK_RULE_KIND_API:
		return enum.RiskRuleKindAPI
	case governancev1.RiskRuleKind_RISK_RULE_KIND_DATABASE:
		return enum.RiskRuleKindDatabase
	case governancev1.RiskRuleKind_RISK_RULE_KIND_SECRET:
		return enum.RiskRuleKindSecret
	case governancev1.RiskRuleKind_RISK_RULE_KIND_AUTH:
		return enum.RiskRuleKindAuth
	case governancev1.RiskRuleKind_RISK_RULE_KIND_RUNTIME_ACTION:
		return enum.RiskRuleKindRuntimeAction
	case governancev1.RiskRuleKind_RISK_RULE_KIND_RELEASE:
		return enum.RiskRuleKindRelease
	case governancev1.RiskRuleKind_RISK_RULE_KIND_AUTOMATION:
		return enum.RiskRuleKindAutomation
	case governancev1.RiskRuleKind_RISK_RULE_KIND_DOCUMENT:
		return enum.RiskRuleKindDocument
	case governancev1.RiskRuleKind_RISK_RULE_KIND_CUSTOM:
		return enum.RiskRuleKindCustom
	default:
		return ""
	}
}

func toRiskRuleKind(item enum.RiskRuleKind) governancev1.RiskRuleKind {
	switch item {
	case enum.RiskRuleKindPath:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_PATH
	case enum.RiskRuleKindService:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_SERVICE
	case enum.RiskRuleKindAPI:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_API
	case enum.RiskRuleKindDatabase:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_DATABASE
	case enum.RiskRuleKindSecret:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_SECRET
	case enum.RiskRuleKindAuth:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_AUTH
	case enum.RiskRuleKindRuntimeAction:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_RUNTIME_ACTION
	case enum.RiskRuleKindRelease:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_RELEASE
	case enum.RiskRuleKindAutomation:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_AUTOMATION
	case enum.RiskRuleKindDocument:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_DOCUMENT
	case enum.RiskRuleKindCustom:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_CUSTOM
	default:
		return governancev1.RiskRuleKind_RISK_RULE_KIND_UNSPECIFIED
	}
}

func ruleStatus(item governancev1.RuleStatus) enum.RuleStatus {
	switch item {
	case governancev1.RuleStatus_RULE_STATUS_ACTIVE:
		return enum.RuleStatusActive
	case governancev1.RuleStatus_RULE_STATUS_DISABLED:
		return enum.RuleStatusDisabled
	default:
		return ""
	}
}

func toRuleStatus(item enum.RuleStatus) governancev1.RuleStatus {
	switch item {
	case enum.RuleStatusActive:
		return governancev1.RuleStatus_RULE_STATUS_ACTIVE
	case enum.RuleStatusDisabled:
		return governancev1.RuleStatus_RULE_STATUS_DISABLED
	default:
		return governancev1.RuleStatus_RULE_STATUS_UNSPECIFIED
	}
}

func gateKind(item governancev1.GateKind) enum.GateKind {
	switch item {
	case governancev1.GateKind_GATE_KIND_PRODUCT:
		return enum.GateKindProduct
	case governancev1.GateKind_GATE_KIND_ARCHITECTURE:
		return enum.GateKindArchitecture
	case governancev1.GateKind_GATE_KIND_TECHNICAL:
		return enum.GateKindTechnical
	case governancev1.GateKind_GATE_KIND_QA:
		return enum.GateKindQA
	case governancev1.GateKind_GATE_KIND_RELEASE:
		return enum.GateKindRelease
	case governancev1.GateKind_GATE_KIND_POSTDEPLOY:
		return enum.GateKindPostdeploy
	case governancev1.GateKind_GATE_KIND_EMERGENCY:
		return enum.GateKindEmergency
	case governancev1.GateKind_GATE_KIND_CUSTOM:
		return enum.GateKindCustom
	default:
		return ""
	}
}

func toGateKind(item enum.GateKind) governancev1.GateKind {
	switch item {
	case enum.GateKindProduct:
		return governancev1.GateKind_GATE_KIND_PRODUCT
	case enum.GateKindArchitecture:
		return governancev1.GateKind_GATE_KIND_ARCHITECTURE
	case enum.GateKindTechnical:
		return governancev1.GateKind_GATE_KIND_TECHNICAL
	case enum.GateKindQA:
		return governancev1.GateKind_GATE_KIND_QA
	case enum.GateKindRelease:
		return governancev1.GateKind_GATE_KIND_RELEASE
	case enum.GateKindPostdeploy:
		return governancev1.GateKind_GATE_KIND_POSTDEPLOY
	case enum.GateKindEmergency:
		return governancev1.GateKind_GATE_KIND_EMERGENCY
	case enum.GateKindCustom:
		return governancev1.GateKind_GATE_KIND_CUSTOM
	default:
		return governancev1.GateKind_GATE_KIND_UNSPECIFIED
	}
}

func riskAssessmentStatus(item governancev1.RiskAssessmentStatus) enum.RiskAssessmentStatus {
	switch item {
	case governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT:
		return enum.RiskAssessmentStatusDraft
	case governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE:
		return enum.RiskAssessmentStatusActive
	case governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED:
		return enum.RiskAssessmentStatusSuperseded
	case governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED:
		return enum.RiskAssessmentStatusClosed
	default:
		return ""
	}
}

func toRiskAssessmentStatus(item enum.RiskAssessmentStatus) governancev1.RiskAssessmentStatus {
	switch item {
	case enum.RiskAssessmentStatusDraft:
		return governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT
	case enum.RiskAssessmentStatusActive:
		return governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE
	case enum.RiskAssessmentStatusSuperseded:
		return governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED
	case enum.RiskAssessmentStatusClosed:
		return governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED
	default:
		return governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_UNSPECIFIED
	}
}

func riskFactorSourceType(item governancev1.RiskFactorSourceType) enum.RiskFactorSourceType {
	switch item {
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY:
		return enum.RiskFactorSourceTypePolicy
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE:
		return enum.RiskFactorSourceTypeChangedFile
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE:
		return enum.RiskFactorSourceTypeService
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API:
		return enum.RiskFactorSourceTypeAPI
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE:
		return enum.RiskFactorSourceTypeDatabase
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET:
		return enum.RiskFactorSourceTypeSecret
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE:
		return enum.RiskFactorSourceTypeRelease
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME:
		return enum.RiskFactorSourceTypeRuntime
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL:
		return enum.RiskFactorSourceTypeReviewSignal
	case governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION:
		return enum.RiskFactorSourceTypeHumanDecision
	default:
		return ""
	}
}

func toRiskFactorSourceType(item enum.RiskFactorSourceType) governancev1.RiskFactorSourceType {
	switch item {
	case enum.RiskFactorSourceTypePolicy:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY
	case enum.RiskFactorSourceTypeChangedFile:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE
	case enum.RiskFactorSourceTypeService:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE
	case enum.RiskFactorSourceTypeAPI:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API
	case enum.RiskFactorSourceTypeDatabase:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE
	case enum.RiskFactorSourceTypeSecret:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET
	case enum.RiskFactorSourceTypeRelease:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE
	case enum.RiskFactorSourceTypeRuntime:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME
	case enum.RiskFactorSourceTypeReviewSignal:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL
	case enum.RiskFactorSourceTypeHumanDecision:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION
	default:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED
	}
}

func reviewRoleKind(item governancev1.ReviewRoleKind) enum.ReviewRoleKind {
	switch item {
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_REVIEWER:
		return enum.ReviewRoleKindReviewer
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_QA:
		return enum.ReviewRoleKindQA
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_LEXICAL_GATEKEEPER:
		return enum.ReviewRoleKindLexicalGatekeeper
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_RISK_GATEKEEPER:
		return enum.ReviewRoleKindRiskGatekeeper
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SRE:
		return enum.ReviewRoleKindSRE
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SECURITY:
		return enum.ReviewRoleKindSecurity
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_OWNER:
		return enum.ReviewRoleKindOwner
	case governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_CUSTOM:
		return enum.ReviewRoleKindCustom
	default:
		return ""
	}
}

func toReviewRoleKind(item enum.ReviewRoleKind) governancev1.ReviewRoleKind {
	switch item {
	case enum.ReviewRoleKindReviewer:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_REVIEWER
	case enum.ReviewRoleKindQA:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_QA
	case enum.ReviewRoleKindLexicalGatekeeper:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_LEXICAL_GATEKEEPER
	case enum.ReviewRoleKindRiskGatekeeper:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_RISK_GATEKEEPER
	case enum.ReviewRoleKindSRE:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SRE
	case enum.ReviewRoleKindSecurity:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SECURITY
	case enum.ReviewRoleKindOwner:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_OWNER
	case enum.ReviewRoleKindCustom:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_CUSTOM
	default:
		return governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_UNSPECIFIED
	}
}

func reviewSignalOutcome(item governancev1.ReviewSignalOutcome) enum.ReviewSignalOutcome {
	switch item {
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS:
		return enum.ReviewSignalOutcomePass
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS_WITH_NOTES:
		return enum.ReviewSignalOutcomePassWithNotes
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_BLOCK:
		return enum.ReviewSignalOutcomeBlock
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_REQUEST_CHANGES:
		return enum.ReviewSignalOutcomeRequestChanges
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_RAISE_RISK:
		return enum.ReviewSignalOutcomeRaiseRisk
	case governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_INFORMATIONAL:
		return enum.ReviewSignalOutcomeInformational
	default:
		return ""
	}
}

func toReviewSignalOutcome(item enum.ReviewSignalOutcome) governancev1.ReviewSignalOutcome {
	switch item {
	case enum.ReviewSignalOutcomePass:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS
	case enum.ReviewSignalOutcomePassWithNotes:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS_WITH_NOTES
	case enum.ReviewSignalOutcomeBlock:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_BLOCK
	case enum.ReviewSignalOutcomeRequestChanges:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_REQUEST_CHANGES
	case enum.ReviewSignalOutcomeRaiseRisk:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_RAISE_RISK
	case enum.ReviewSignalOutcomeInformational:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_INFORMATIONAL
	default:
		return governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_UNSPECIFIED
	}
}

func signalSeverity(item governancev1.SignalSeverity) enum.SignalSeverity {
	switch item {
	case governancev1.SignalSeverity_SIGNAL_SEVERITY_INFO:
		return enum.SignalSeverityInfo
	case governancev1.SignalSeverity_SIGNAL_SEVERITY_WARNING:
		return enum.SignalSeverityWarning
	case governancev1.SignalSeverity_SIGNAL_SEVERITY_BLOCKING:
		return enum.SignalSeverityBlocking
	case governancev1.SignalSeverity_SIGNAL_SEVERITY_CRITICAL:
		return enum.SignalSeverityCritical
	default:
		return ""
	}
}

func toSignalSeverity(item enum.SignalSeverity) governancev1.SignalSeverity {
	switch item {
	case enum.SignalSeverityInfo:
		return governancev1.SignalSeverity_SIGNAL_SEVERITY_INFO
	case enum.SignalSeverityWarning:
		return governancev1.SignalSeverity_SIGNAL_SEVERITY_WARNING
	case enum.SignalSeverityBlocking:
		return governancev1.SignalSeverity_SIGNAL_SEVERITY_BLOCKING
	case enum.SignalSeverityCritical:
		return governancev1.SignalSeverity_SIGNAL_SEVERITY_CRITICAL
	default:
		return governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED
	}
}

func confidence(item governancev1.Confidence) enum.Confidence {
	switch item {
	case governancev1.Confidence_CONFIDENCE_LOW:
		return enum.ConfidenceLow
	case governancev1.Confidence_CONFIDENCE_MEDIUM:
		return enum.ConfidenceMedium
	case governancev1.Confidence_CONFIDENCE_HIGH:
		return enum.ConfidenceHigh
	default:
		return ""
	}
}

func toConfidence(item enum.Confidence) governancev1.Confidence {
	switch item {
	case enum.ConfidenceLow:
		return governancev1.Confidence_CONFIDENCE_LOW
	case enum.ConfidenceMedium:
		return governancev1.Confidence_CONFIDENCE_MEDIUM
	case enum.ConfidenceHigh:
		return governancev1.Confidence_CONFIDENCE_HIGH
	default:
		return governancev1.Confidence_CONFIDENCE_UNSPECIFIED
	}
}

func evidenceKindString(item governancev1.EvidenceKind) string {
	switch item {
	case governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT:
		return "provider_comment"
	case governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW:
		return "provider_review"
	case governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK:
		return "provider_check"
	case governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY:
		return "runtime_summary"
	case governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT:
		return "document"
	case governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR:
		return "risk_factor"
	case governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL:
		return "review_signal"
	case governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK:
		return "interaction_callback"
	case governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF:
		return "object_ref"
	case governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM:
		return "custom"
	default:
		return ""
	}
}

func toEvidenceKind(item string) governancev1.EvidenceKind {
	switch item {
	case "provider_comment":
		return governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT
	case "provider_review":
		return governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW
	case "provider_check":
		return governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK
	case "runtime_summary":
		return governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY
	case "document":
		return governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT
	case "risk_factor":
		return governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR
	case "review_signal":
		return governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL
	case "interaction_callback":
		return governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK
	case "object_ref":
		return governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF
	case "custom":
		return governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM
	default:
		return governancev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED
	}
}

func gateRequestStatus(item governancev1.GateRequestStatus) enum.GateRequestStatus {
	switch item {
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED:
		return enum.GateRequestStatusRequested
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING:
		return enum.GateRequestStatusDelivering
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION:
		return enum.GateRequestStatusAwaitingDecision
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED:
		return enum.GateRequestStatusResolved
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED:
		return enum.GateRequestStatusExpired
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED:
		return enum.GateRequestStatusCancelled
	default:
		return ""
	}
}

func toGateRequestStatus(item enum.GateRequestStatus) governancev1.GateRequestStatus {
	switch item {
	case enum.GateRequestStatusRequested:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED
	case enum.GateRequestStatusDelivering:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING
	case enum.GateRequestStatusAwaitingDecision:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION
	case enum.GateRequestStatusResolved:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED
	case enum.GateRequestStatusExpired:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED
	case enum.GateRequestStatusCancelled:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED
	default:
		return governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED
	}
}

func gateOutcome(item governancev1.GateOutcome) enum.GateOutcome {
	switch item {
	case governancev1.GateOutcome_GATE_OUTCOME_APPROVE:
		return enum.GateOutcomeApprove
	case governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS:
		return enum.GateOutcomeApproveWithConditions
	case governancev1.GateOutcome_GATE_OUTCOME_REVISE:
		return enum.GateOutcomeRevise
	case governancev1.GateOutcome_GATE_OUTCOME_REJECT:
		return enum.GateOutcomeReject
	case governancev1.GateOutcome_GATE_OUTCOME_HOLD:
		return enum.GateOutcomeHold
	case governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK:
		return enum.GateOutcomeRollback
	case governancev1.GateOutcome_GATE_OUTCOME_ESCALATE:
		return enum.GateOutcomeEscalate
	default:
		return ""
	}
}

func toGateOutcome(item enum.GateOutcome) governancev1.GateOutcome {
	switch item {
	case enum.GateOutcomeApprove:
		return governancev1.GateOutcome_GATE_OUTCOME_APPROVE
	case enum.GateOutcomeApproveWithConditions:
		return governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS
	case enum.GateOutcomeRevise:
		return governancev1.GateOutcome_GATE_OUTCOME_REVISE
	case enum.GateOutcomeReject:
		return governancev1.GateOutcome_GATE_OUTCOME_REJECT
	case enum.GateOutcomeHold:
		return governancev1.GateOutcome_GATE_OUTCOME_HOLD
	case enum.GateOutcomeRollback:
		return governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK
	case enum.GateOutcomeEscalate:
		return governancev1.GateOutcome_GATE_OUTCOME_ESCALATE
	default:
		return governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED
	}
}

func releaseDecisionPackageStatus(item governancev1.ReleaseDecisionPackageStatus) enum.ReleaseDecisionPackageStatus {
	switch item {
	case governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DRAFT:
		return enum.ReleaseDecisionPackageStatusDraft
	case governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_READY:
		return enum.ReleaseDecisionPackageStatusReady
	case governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DECISION_REQUESTED:
		return enum.ReleaseDecisionPackageStatusDecisionRequested
	case governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_CLOSED:
		return enum.ReleaseDecisionPackageStatusClosed
	default:
		return ""
	}
}

func toReleaseDecisionPackageStatus(item enum.ReleaseDecisionPackageStatus) governancev1.ReleaseDecisionPackageStatus {
	switch item {
	case enum.ReleaseDecisionPackageStatusDraft:
		return governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DRAFT
	case enum.ReleaseDecisionPackageStatusReady:
		return governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_READY
	case enum.ReleaseDecisionPackageStatusDecisionRequested:
		return governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DECISION_REQUESTED
	case enum.ReleaseDecisionPackageStatusClosed:
		return governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_CLOSED
	default:
		return governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_UNSPECIFIED
	}
}
