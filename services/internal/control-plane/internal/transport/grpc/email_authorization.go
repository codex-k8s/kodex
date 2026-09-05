package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ResolveEmailAuthorization(ctx context.Context, request *cp.ResolveEmailAuthorizationRequest) (*cp.ResolveEmailAuthorizationResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_ResolveEmailAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := emailBindingFromProto(request.GetBinding())
	if err != nil {
		return nil, transportError(err)
	}
	op := emailOperationFromProto(request.GetOperation())
	if op == "" {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.ResolveEmailAuthorization(ctx, p, query.EmailAuthorization{
		Binding: binding, Operation: op, MailboxRef: request.GetMailboxRef(), ConfigurationRevision: request.GetConfigurationRevision(),
		SemanticInputDigest: request.GetSemanticInputDigest(), EffectKey: request.GetEffectKey(), Sender: request.GetSender(),
		Folder: request.GetFolder(), DestinationFolder: request.GetDestinationFolder(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	policy, ok := cp.EmailApprovalPolicy_value["EMAIL_APPROVAL_POLICY_"+strings.ToUpper(result.Policy)]
	if !ok || policy == 0 || result.ExpiresAt.IsZero() || result.ActorRef == "" || result.GrantRef == "" {
		return nil, transportError(errs.ErrUnavailable)
	}
	response := &cp.ResolveEmailAuthorizationResponse{Allowed: result.Allowed, ActorRef: result.ActorRef, AgentRef: result.AgentRef,
		OrganizationRef: result.OrganizationRef, ProjectRef: result.ProjectRef, ConnectionRef: result.ConnectionRef,
		MailboxRef: result.MailboxRef, GrantRef: result.GrantRef, Operation: emailOperationProto(result.Operation),
		SemanticInputDigest: result.SemanticInputDigest, EffectKey: result.EffectKey, ConfigurationRevision: result.ConfigurationRevision,
		CredentialGeneration: result.CredentialGeneration, Policy: cp.EmailApprovalPolicy(policy), GateApproved: result.GateApproved,
		UserScope: castEmailScope(result.UserScope), ConnectionScope: castEmailScope(result.ConnectionScope),
		ResourceScope: castEmailScope(result.ResourceScope), ExpiresAt: timestamppb.New(result.ExpiresAt), Binding: castEmailBinding(result.Binding)}
	if result.AgentScope != nil {
		response.AgentScope = castEmailScope(*result.AgentScope)
	}
	return response, nil
}

func emailBindingFromProto(input *cp.EmailExecutionBinding) (entity.EmailExecutionBinding, error) {
	if input == nil || (input.GetInvocationRef() == "") == (input.GetConnectionTestRef() == "") || input.GetLease() == nil || input.GetLease().GetExpiresAt() == nil || input.GetLease().GetExpiresAt().CheckValid() != nil {
		return entity.EmailExecutionBinding{}, errs.ErrInvalid
	}
	lease := input.GetLease()
	return entity.EmailExecutionBinding{InvocationRef: input.GetInvocationRef(), ConnectionTestRef: input.GetConnectionTestRef(),
		LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), ExpiresAt: lease.GetExpiresAt().AsTime()}, nil
}

func castEmailBinding(input entity.EmailExecutionBinding) *cp.EmailExecutionBinding {
	result := &cp.EmailExecutionBinding{Lease: &cp.WorkLease{Ref: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, ExpiresAt: timestamppb.New(input.ExpiresAt)}}
	if input.InvocationRef != "" {
		result.Source = &cp.EmailExecutionBinding_InvocationRef{InvocationRef: input.InvocationRef}
	} else {
		result.Source = &cp.EmailExecutionBinding_ConnectionTestRef{ConnectionTestRef: input.ConnectionTestRef}
	}
	return result
}

func emailOperationFromProto(input cp.EmailOperation) string {
	name, ok := cp.EmailOperation_name[int32(input)]
	if !ok || input == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(name, "EMAIL_OPERATION_"))
}

func emailOperationProto(input string) cp.EmailOperation {
	return cp.EmailOperation(cp.EmailOperation_value["EMAIL_OPERATION_"+strings.ToUpper(input)])
}

func castEmailScope(input entity.EmailAuthorizationScope) *cp.EmailAuthorizationScope {
	result := &cp.EmailAuthorizationScope{MailboxRef: input.MailboxRef, Sender: input.Sender, Folders: input.Folders, Recipients: input.Recipients, Operations: []cp.EmailOperation{}}
	for _, operation := range input.Operations {
		result.Operations = append(result.Operations, emailOperationProto(operation))
	}
	return result
}
