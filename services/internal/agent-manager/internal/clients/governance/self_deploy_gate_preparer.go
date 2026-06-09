// Package governance адаптирует операции governance-manager для agent-manager.
package governance

import (
	"context"
	"strings"
	"time"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	callerID              = "agent-manager"
	defaultRequestTimeout = 10 * time.Second
)

// Config содержит настройки клиента governance-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type selfDeployGateClient interface {
	PrepareSelfDeployPlanGate(context.Context, *governancev1.PrepareSelfDeployPlanGateRequest, ...grpc.CallOption) (*governancev1.SelfDeployPlanGateResponse, error)
}

// SelfDeployGatePreparer вызывает governance-manager PrepareSelfDeployPlanGate.
type SelfDeployGatePreparer struct {
	client    selfDeployGateClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.SelfDeployGatePreparer = (*SelfDeployGatePreparer)(nil)

// NewConnection создаёт gRPC-подключение к governance-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "governance-manager")
}

// NewSelfDeployGatePreparer создаёт клиент подготовки self-deploy gate.
func NewSelfDeployGatePreparer(client governancev1.GovernanceManagerServiceClient, cfg Config) (*SelfDeployGatePreparer, error) {
	return newSelfDeployGatePreparer(client, cfg)
}

func newSelfDeployGatePreparer(client selfDeployGateClient, cfg Config) (*SelfDeployGatePreparer, error) {
	settings, err := selfDeployGateSettings(client, cfg)
	if err != nil {
		return nil, err
	}
	preparer := &SelfDeployGatePreparer{client: client}
	preparer.authToken = settings.AuthToken
	preparer.timeout = settings.Timeout
	return preparer, nil
}

func selfDeployGateSettings(client selfDeployGateClient, cfg Config) (grpcclient.ClientSettings, error) {
	return grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultRequestTimeout, "governance-manager")
}

// PrepareSelfDeployPlanGate готовит или переиспользует owner/governance gate для self-deploy plan.
func (preparer *SelfDeployGatePreparer) PrepareSelfDeployPlanGate(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput) (agentservice.SelfDeployPlanGatePreparationResult, error) {
	if preparer == nil || preparer.client == nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(grpcclient.OutgoingContext(ctx, preparer.authToken, callerID), preparer.timeout)
	defer cancel()
	response, err := preparer.client.PrepareSelfDeployPlanGate(callCtx, prepareSelfDeployPlanGateRequest(input))
	if err != nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, grpcclient.MapReadError(err, "governance-manager self-deploy gate command failed")
	}
	return selfDeployPlanGateResult(response)
}

func prepareSelfDeployPlanGateRequest(input agentservice.SelfDeployPlanGatePreparationInput) *governancev1.PrepareSelfDeployPlanGateRequest {
	plan := input.Plan
	return &governancev1.PrepareSelfDeployPlanGateRequest{
		Meta: commandMeta(input.Meta),
		Plan: &governancev1.SelfDeployPlanGateInput{
			SelfDeployPlanRef:       selfDeployPlanRef(plan.ID),
			ProjectContext:          projectContext(plan),
			ProviderSignalRef:       optionalString(plan.ProviderSignalRef),
			SourceRef:               optionalString(plan.SourceRef),
			MergeCommitSha:          optionalString(plan.MergeCommitSHA),
			ServicesYamlRef:         optionalString(plan.ServicesYAMLRef),
			ServicesYamlDigest:      optionalString(plan.ServicesYAMLDigest),
			AffectedServiceKeys:     append([]string(nil), plan.AffectedServiceKeys...),
			PathCategories:          selfDeployPathCategories(plan.PathCategories),
			ExpectedRuntimeJobTypes: selfDeployRuntimeJobTypes(plan.ExpectedRuntimeJobTypes),
			SafeSummary:             optionalString(plan.SafeSummary),
			PlanFingerprint:         strings.TrimSpace(plan.PlanFingerprint),
			RiskProfileRef:          optionalString(governanceRiskProfileRef(plan.GovernanceContext.RiskProfileRef)),
		},
	}
}

func commandMeta(meta value.CommandMeta) *governancev1.CommandMeta {
	commandID := optionalUUIDString(meta.CommandID)
	idempotencyKey := optionalString(meta.IdempotencyKey)
	requestID := firstNonEmpty(optionalStringValue(commandID), strings.TrimSpace(meta.IdempotencyKey), "self-deploy-plan-gate")
	return &governancev1.CommandMeta{
		CommandId:      commandID,
		IdempotencyKey: idempotencyKey,
		Actor:          actor(meta.Actor),
		Reason:         "agent-self-deploy-plan-gate",
		RequestId:      requestID,
		RequestContext: &governancev1.RequestContext{Source: callerID},
	}
}

func actor(item value.Actor) *governancev1.Actor {
	if strings.TrimSpace(item.Type) == "" && strings.TrimSpace(item.ID) == "" {
		return nil
	}
	return &governancev1.Actor{Type: strings.TrimSpace(item.Type), Id: strings.TrimSpace(item.ID)}
}

func projectContext(plan entity.SelfDeployPlan) *governancev1.ProjectContextRef {
	return &governancev1.ProjectContextRef{
		ProjectRef:       optionalString(plan.ProjectRef),
		RepositoryRef:    optionalString(plan.RepositoryRef),
		ReleasePolicyRef: optionalString(plan.GovernanceContext.ReleasePolicyRef),
	}
}

func selfDeployPlanGateResult(response *governancev1.SelfDeployPlanGateResponse) (agentservice.SelfDeployPlanGatePreparationResult, error) {
	if response == nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	context := value.GovernanceContextRef{
		RiskAssessmentRef: governanceRef("risk_assessment", response.GetRiskAssessment().GetId()),
		GateRequestRef:    governanceRef("gate_request", response.GetGateRequest().GetId()),
	}
	if decision := response.GetGateDecision(); decision != nil {
		context.GateDecisionRef = governanceRef("gate_decision", decision.GetId())
	}
	status, ok := selfDeployPlanGateStatus(response.GetStatus())
	if !ok {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	if context.RiskAssessmentRef == "" {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	if status == agentservice.SelfDeployPlanGateStatusPending && context.GateRequestRef == "" {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	return agentservice.SelfDeployPlanGatePreparationResult{
		Status:            status,
		GovernanceContext: context,
		SafeSummary:       strings.TrimSpace(response.GetRiskAssessment().GetExplanation()),
	}, nil
}

func selfDeployPlanGateStatus(status governancev1.SelfDeployPlanGateStatus) (agentservice.SelfDeployPlanGateStatus, bool) {
	switch status {
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_PENDING:
		return agentservice.SelfDeployPlanGateStatusPending, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_APPROVED:
		return agentservice.SelfDeployPlanGateStatusApproved, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_REJECTED:
		return agentservice.SelfDeployPlanGateStatusRejected, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_BLOCKED:
		return agentservice.SelfDeployPlanGateStatusBlocked, true
	default:
		return "", false
	}
}

func selfDeployPlanRef(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return "agent:self-deploy-plan:" + id.String()
}

func governanceRef(kind string, id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	return "governance:" + kind + "/" + trimmed
}

func governanceRiskProfileRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	for _, prefix := range []string{"governance:risk_profile/", "risk_profile/", "governance:risk_profile:", "risk_profile:"} {
		if suffix, ok := strings.CutPrefix(trimmed, prefix); ok {
			return governanceRiskProfileUUIDRef(suffix, "governance:risk_profile:")
		}
	}
	return governanceRiskProfileUUIDRef(trimmed, "")
}

func governanceRiskProfileUUIDRef(value string, prefix string) string {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || id == uuid.Nil {
		return ""
	}
	return prefix + id.String()
}

func selfDeployPathCategories(values []enum.SelfDeployPathCategory) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func selfDeployRuntimeJobTypes(values []enum.SelfDeployRuntimeJobType) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func optionalUUIDString(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
