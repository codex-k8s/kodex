package authority

import (
	"context"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Client struct{ API cp.RuntimeWorkServiceClient }

func Binding(b *api.ExecutionBinding) *cp.EmailExecutionBinding {
	if !api.ValidExecutionBinding(b) {
		return nil
	}
	result := &cp.EmailExecutionBinding{Lease: &cp.WorkLease{Ref: b.Lease.Ref, Fence: b.Lease.Fence, Generation: b.Lease.Generation, ExpiresAt: timestamppb.New(b.Lease.ExpiresAt)}}
	if b.InvocationRef != nil {
		result.Source = &cp.EmailExecutionBinding_InvocationRef{InvocationRef: *b.InvocationRef}
	} else {
		result.Source = &cp.EmailExecutionBinding_ConnectionTestRef{ConnectionTestRef: *b.ConnectionTestRef}
	}
	return result
}

func (c *Client) Resolve(ctx context.Context, input api.AuthorizationRequest) (api.AuthorizationDecision, error) {
	binding := Binding(input.ExecutionBinding)
	if binding == nil || binding.Lease.ExpiresAt.CheckValid() != nil || !binding.Lease.ExpiresAt.AsTime().After(time.Now()) || input.InvocationToken != binding.Lease.Fence {
		return api.AuthorizationDecision{}, errs.Denied
	}
	op, ok := operation(input.Operation)
	if !ok || binding.GetConnectionTestRef() != "" && input.Operation != api.OperationHealth {
		return api.AuthorizationDecision{}, errs.Denied
	}
	ctx, cancel := context.WithDeadline(ctx, binding.Lease.ExpiresAt.AsTime())
	defer cancel()
	response, err := c.API.ResolveEmailAuthorization(ctx, &cp.ResolveEmailAuthorizationRequest{Binding: binding, MailboxRef: input.MailboxId, ConfigurationRevision: input.ConfigurationRevision, Operation: op, SemanticInputDigest: input.InputSha256, EffectKey: input.EffectKey, Sender: input.Sender, Folder: input.Folder, DestinationFolder: input.DestinationFolder})
	if err != nil {
		if status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.Unauthenticated {
			return api.AuthorizationDecision{}, errs.Denied
		}
		return api.AuthorizationDecision{}, errs.Unavailable
	}
	if response == nil || !proto.Equal(response.Binding, binding) || response.Operation != op || response.ExpiresAt == nil || response.ExpiresAt.CheckValid() != nil || response.ExpiresAt.AsTime().After(binding.Lease.ExpiresAt.AsTime()) {
		return api.AuthorizationDecision{}, errs.Unavailable
	}
	policy := map[cp.EmailApprovalPolicy]api.Policy{cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_ALLOW: api.Allow, cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_DENY: api.Deny, cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_HUMAN_GATE: api.HumanGate}
	approval, ok := policy[response.Policy]
	if !ok {
		return api.AuthorizationDecision{}, errs.Unavailable
	}
	user, uok := scope(response.UserScope)
	agent, aok := scope(response.AgentScope)
	connection, cok := scope(response.ConnectionScope)
	resource, rok := scope(response.ResourceScope)
	if binding.GetConnectionTestRef() != "" {
		aok = response.AgentRef == "" && response.AgentScope == nil
	}
	if !uok || !aok || !cok || !rok {
		return api.AuthorizationDecision{}, errs.Unavailable
	}
	return api.AuthorizationDecision{Allowed: response.Allowed, ActorId: response.ActorRef, AgentId: response.AgentRef, TenantId: response.OrganizationRef, ConnectionId: response.ConnectionRef, MailboxId: response.MailboxRef, GrantId: response.GrantRef, Operation: input.Operation, InputSha256: response.SemanticInputDigest, EffectKey: response.EffectKey, ConfigurationRevision: response.ConfigurationRevision, CredentialGeneration: response.CredentialGeneration, Policy: approval, GateApproved: response.GateApproved, UserScope: user, AgentScope: agent, ConnectionScope: connection, ResourceScope: resource, ExpiresAt: response.ExpiresAt.AsTime().Unix(), ExecutionBinding: input.ExecutionBinding}, nil
}

func operation(op api.Operation) (cp.EmailOperation, bool) {
	value, ok := cp.EmailOperation_value["EMAIL_OPERATION_"+strings.ToUpper(string(op))]
	return cp.EmailOperation(value), ok && value != 0
}
func scope(s *cp.EmailAuthorizationScope) (api.Scope, bool) {
	if s == nil {
		return api.Scope{}, false
	}
	result := api.Scope{MailboxId: s.MailboxRef, Sender: s.Sender, Folders: append([]string{}, s.Folders...), Recipients: append([]string{}, s.Recipients...)}
	for _, op := range s.Operations {
		value := api.Operation(strings.ToLower(strings.TrimPrefix(op.String(), "EMAIL_OPERATION_")))
		if !value.Valid() {
			return api.Scope{}, false
		}
		result.Operations = append(result.Operations, value)
	}
	return result, true
}
