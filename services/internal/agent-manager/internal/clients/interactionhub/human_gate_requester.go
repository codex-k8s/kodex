// Package interactionhub adapts interaction-hub owner requests to agent-manager.
package interactionhub

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	callerID              = "agent-manager"
	defaultRequestTimeout = 10 * time.Second
)

// Config contains interaction-hub client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type interactionHubClient interface {
	RequestHumanGate(context.Context, *interactionsv1.RequestHumanGateRequest, ...grpc.CallOption) (*interactionsv1.InteractionRequestResponse, error)
}

// HumanGateRequester calls interaction-hub RequestHumanGate.
type HumanGateRequester struct {
	client    interactionHubClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.HumanGateInteractionRequester = (*HumanGateRequester)(nil)

// NewConnection creates a gRPC connection to interaction-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "interaction-hub")
}

// NewHumanGateRequester creates an interaction-hub Human gate request client.
func NewHumanGateRequester(client interactionsv1.InteractionHubServiceClient, cfg Config) (*HumanGateRequester, error) {
	return newHumanGateRequester(client, cfg)
}

func newHumanGateRequester(client interactionHubClient, cfg Config) (*HumanGateRequester, error) {
	settings, err := grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultRequestTimeout, "interaction-hub")
	switch {
	case err != nil:
		return nil, err
	default:
		return humanGateRequesterWithSettings(client, settings), nil
	}
}

func humanGateRequesterWithSettings(client interactionHubClient, settings grpcclient.ClientSettings) *HumanGateRequester {
	return &HumanGateRequester{
		client:    client,
		authToken: settings.AuthToken,
		timeout:   settings.Timeout,
	}
}

// RequestHumanGate creates an owner-visible Human gate request in interaction-hub.
func (requester *HumanGateRequester) RequestHumanGate(ctx context.Context, input agentservice.HumanGateInteractionRequestInput) (agentservice.HumanGateInteractionRequestResult, error) {
	return requester.call(ctx, func(callCtx context.Context) (agentservice.HumanGateInteractionRequestResult, error) {
		response, err := requester.client.RequestHumanGate(callCtx, requestHumanGateRequest(input))
		if err != nil {
			return agentservice.HumanGateInteractionRequestResult{}, mapInteractionHubRequestError(err)
		}
		return humanGateRequestResult(response)
	})
}

func (requester *HumanGateRequester) call(ctx context.Context, execute func(context.Context) (agentservice.HumanGateInteractionRequestResult, error)) (agentservice.HumanGateInteractionRequestResult, error) {
	if requester == nil || requester.client == nil {
		return agentservice.HumanGateInteractionRequestResult{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(grpcclient.OutgoingContext(ctx, requester.authToken, callerID), requester.timeout)
	defer cancel()
	return execute(callCtx)
}

func requestHumanGateRequest(input agentservice.HumanGateInteractionRequestInput) *interactionsv1.RequestHumanGateRequest {
	return &interactionsv1.RequestHumanGateRequest{
		Meta: commandMeta(input.Meta),
		Request: &interactionsv1.InteractionRequestDraft{
			Scope:             scopeRef(input.Scope),
			SourceOwner:       sourceOwnerRef(input.SourceOwnerRef),
			Ingress:           ingressRef(input.IngressRef),
			DecisionOwner:     decisionOwnerRef(input.HumanGateRequestID),
			TargetRefs:        actorRefs(input.TargetRefs),
			ContextRefs:       externalRefs(input.ContextRefs),
			PromptSummary:     strings.TrimSpace(input.PromptSummary),
			AllowedActions:    interactionActions(input.AllowedActions),
			RiskClass:         riskClass(input.RiskClass),
			ReminderPolicyRef: optionalString(input.ReminderPolicyRef),
		},
	}
}

func commandMeta(meta value.CommandMeta) *interactionsv1.CommandMeta {
	commandID := optionalUUIDString(meta.CommandID.String())
	idempotencyKey := optionalString(meta.IdempotencyKey)
	requestID := firstNonEmpty(optionalStringValue(commandID), strings.TrimSpace(meta.IdempotencyKey), "human-gate-request")
	return &interactionsv1.CommandMeta{
		CommandId:      commandID,
		IdempotencyKey: idempotencyKey,
		Actor:          actor(meta.Actor),
		Reason:         "agent-human-gate-request",
		RequestId:      requestID,
		RequestContext: &interactionsv1.RequestContext{Source: callerID},
	}
}

func actor(actor value.Actor) *interactionsv1.Actor {
	return &interactionsv1.Actor{Type: strings.TrimSpace(actor.Type), Id: strings.TrimSpace(actor.ID)}
}

func scopeRef(scope value.ScopeRef) *interactionsv1.ScopeRef {
	return &interactionsv1.ScopeRef{Type: scopeType(scope.Type), Ref: strings.TrimSpace(scope.Ref)}
}

func sourceOwnerRef(ref string) *interactionsv1.SourceOwnerRef {
	return &interactionsv1.SourceOwnerRef{
		Kind: interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER,
		Ref:  optionalString(ref),
	}
}

func ingressRef(ref string) *interactionsv1.IngressRef {
	return &interactionsv1.IngressRef{
		Kind: interactionsv1.IngressKind_INGRESS_KIND_SERVICE,
		Ref:  optionalString(ref),
	}
}

func decisionOwnerRef(gateID uuidLike) *interactionsv1.DecisionOwnerRef {
	return &interactionsv1.DecisionOwnerRef{
		OwnerKind:       interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER,
		OwnerRequestRef: "agent:human_gate/" + gateID.String(),
	}
}

type uuidLike interface {
	String() string
}

func actorRefs(refs []agentservice.HumanGateInteractionActorRef) []*interactionsv1.ActorRef {
	return mapRefs(refs, func(ref agentservice.HumanGateInteractionActorRef) *interactionsv1.ActorRef {
		return &interactionsv1.ActorRef{
			RefKind: strings.TrimSpace(ref.Kind),
			Ref:     strings.TrimSpace(ref.Ref),
		}
	})
}

func externalRefs(refs []agentservice.HumanGateInteractionExternalRef) []*interactionsv1.ExternalRef {
	result := make([]*interactionsv1.ExternalRef, len(refs))
	for index := range refs {
		result[index] = &interactionsv1.ExternalRef{RefKind: strings.TrimSpace(refs[index].Kind), Ref: strings.TrimSpace(refs[index].Ref)}
	}
	return result
}

func interactionActions(actions []agentservice.HumanGateInteractionAction) []*interactionsv1.InteractionAction {
	result := make([]*interactionsv1.InteractionAction, 0, len(actions))
	for _, action := range actions {
		result = append(result, &interactionsv1.InteractionAction{
			ActionKey:        strings.TrimSpace(action.ActionKey),
			LabelTemplateRef: optionalString(action.LabelTemplateRef),
			IsTerminal:       action.Terminal,
		})
	}
	return result
}

func mapRefs[T any, R any](refs []T, build func(T) R) []R {
	if len(refs) == 0 {
		return nil
	}
	result := make([]R, len(refs))
	for index := range refs {
		result[index] = build(refs[index])
	}
	return result
}

func scopeType(value string) interactionsv1.InteractionScopeType {
	switch strings.TrimSpace(value) {
	case "platform":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM
	case "organization":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION
	case "project":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT
	case "repository":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY
	case "service":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE
	default:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_UNSPECIFIED
	}
}

func riskClass(value string) interactionsv1.InteractionRiskClass {
	switch strings.TrimSpace(value) {
	case "low":
		return interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_LOW
	case "medium":
		return interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_MEDIUM
	case "high":
		return interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_HIGH
	case "critical":
		return interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_CRITICAL
	default:
		return interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_UNSPECIFIED
	}
}

func humanGateRequestResult(response *interactionsv1.InteractionRequestResponse) (agentservice.HumanGateInteractionRequestResult, error) {
	if response == nil || response.GetRequest() == nil {
		return agentservice.HumanGateInteractionRequestResult{}, errs.ErrDependencyUnavailable
	}
	request := response.GetRequest()
	requestID := strings.TrimSpace(request.GetId())
	if requestID == "" {
		return agentservice.HumanGateInteractionRequestResult{}, errs.ErrDependencyUnavailable
	}
	return agentservice.HumanGateInteractionRequestResult{
		InteractionRequestRef: "interaction:request/" + requestID,
		Status:                interactionRequestStatus(request.GetStatus()),
		SafeSummary:           strings.TrimSpace(request.GetPromptSummary()),
		Version:               request.GetVersion(),
	}, nil
}

func interactionRequestStatus(status interactionsv1.InteractionRequestStatus) string {
	switch status {
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CREATED:
		return "created"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ROUTED:
		return "routed"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING:
		return "waiting"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED:
		return "answered"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_EXPIRED:
		return "expired"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED:
		return "cancelled"
	case interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_FAILED:
		return "failed"
	default:
		return ""
	}
}

func mapInteractionHubRequestError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return grpcclient.MapReadError(err, "interaction-hub human gate request failed")
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

func optionalUUIDString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "00000000-0000-0000-0000-000000000000" {
		return nil
	}
	return &trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
