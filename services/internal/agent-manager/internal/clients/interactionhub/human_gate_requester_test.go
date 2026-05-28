package interactionhub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func TestHumanGateRequesterMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("91919191-1111-2222-3333-444444444444")
	gateID := uuid.MustParse("91919191-2222-3333-4444-555555555555")
	client := &fakeInteractionHubClient{
		response: &interactionsv1.InteractionRequestResponse{
			Request: &interactionsv1.InteractionRequest{
				Id:            "request-1",
				Status:        interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING,
				PromptSummary: "Safe gate summary",
				Version:       1,
			},
		},
	}
	requester, err := newHumanGateRequester(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newHumanGateRequester() err = %v", err)
	}

	result, err := requester.RequestHumanGate(context.Background(), agentservice.HumanGateInteractionRequestInput{
		Meta:               value.CommandMeta{CommandID: commandID, Actor: value.Actor{Type: "user", ID: "owner"}},
		HumanGateRequestID: gateID,
		Scope:              value.ScopeRef{Type: "project", Ref: "project:alpha"},
		SourceOwnerRef:     "agent:human_gate/" + gateID.String(),
		IngressRef:         "agent-command:" + commandID.String(),
		PromptSummary:      "Safe gate summary",
		TargetRefs:         []agentservice.HumanGateInteractionActorRef{{Kind: "user", Ref: "owner"}},
		ContextRefs:        []agentservice.HumanGateInteractionExternalRef{{Kind: "agent_session", Ref: "session-1"}},
		AllowedActions: []agentservice.HumanGateInteractionAction{
			{ActionKey: "approve", LabelTemplateRef: "interaction.actions.approve", Terminal: true},
			{ActionKey: "reject", LabelTemplateRef: "interaction.actions.reject", Terminal: true},
			{ActionKey: "request_changes", LabelTemplateRef: "interaction.actions.request_changes", Terminal: true},
			{ActionKey: "answer", LabelTemplateRef: "interaction.actions.answer", Terminal: true},
		},
		RiskClass: "low",
	})
	if err != nil {
		t.Fatalf("RequestHumanGate() err = %v", err)
	}
	if result.InteractionRequestRef != "interaction:request/request-1" || result.Status != "waiting" || result.Version != 1 {
		t.Fatalf("result = %+v", result)
	}
	request := client.request
	if request.GetMeta().GetCommandId() != commandID.String() || request.GetMeta().GetActor().GetId() != "owner" {
		t.Fatalf("meta = %+v", request.GetMeta())
	}
	draft := request.GetRequest()
	if draft.GetScope().GetType() != interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT ||
		draft.GetSourceOwner().GetKind() != interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER ||
		draft.GetDecisionOwner().GetOwnerKind() != interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER ||
		draft.GetDecisionOwner().GetOwnerRequestRef() != "agent:human_gate/"+gateID.String() {
		t.Fatalf("draft owner/scope = %+v", draft)
	}
	if len(draft.GetTargetRefs()) != 1 || draft.GetTargetRefs()[0].GetRefKind() != "user" || len(draft.GetAllowedActions()) != 4 {
		t.Fatalf("draft refs/actions = %+v", draft)
	}
	if draft.GetAllowedActions()[2].GetActionKey() != "request_changes" ||
		draft.GetAllowedActions()[2].GetLabelTemplateRef() != "interaction.actions.request_changes" ||
		!draft.GetAllowedActions()[2].GetIsTerminal() {
		t.Fatalf("request_changes action = %+v", draft.GetAllowedActions()[2])
	}
}

func TestHumanGateRequesterMapsInteractionHubErrors(t *testing.T) {
	t.Parallel()

	requester, err := newHumanGateRequester(&fakeInteractionHubClient{err: status.Error(codes.InvalidArgument, "bad request")}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newHumanGateRequester() err = %v", err)
	}
	_, err = requester.RequestHumanGate(context.Background(), agentservice.HumanGateInteractionRequestInput{})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestHumanGateRequesterRejectsEmptyResponse(t *testing.T) {
	t.Parallel()

	requester, err := newHumanGateRequester(&fakeInteractionHubClient{}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newHumanGateRequester() err = %v", err)
	}
	_, err = requester.RequestHumanGate(context.Background(), agentservice.HumanGateInteractionRequestInput{})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
}

type fakeInteractionHubClient struct {
	request  *interactionsv1.RequestHumanGateRequest
	response *interactionsv1.InteractionRequestResponse
	err      error
}

func (f *fakeInteractionHubClient) RequestHumanGate(_ context.Context, request *interactionsv1.RequestHumanGateRequest, _ ...grpc.CallOption) (*interactionsv1.InteractionRequestResponse, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}
