package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/proto"
)

// knownPersonalEmailRequest rejects unknown wire fields before semantic setup processing; optional mutation metadata is checked independently rather than silently discarded.
// knownPersonalEmailRequest отклоняет неизвестные wire fields до смысловой обработки настройки; необязательные mutation metadata проверяются отдельно, а не молча отбрасываются.
func knownPersonalEmailRequest(request proto.Message, mutation *cp.MutationContext) bool {
	if request == nil || !request.ProtoReflect().IsValid() || len(request.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	return mutation == nil || len(mutation.ProtoReflect().GetUnknown()) == 0
}

// GetPersonalEmailMailboxMutationReceipt maps only the closed setup receipt kinds and a single typed result; no historical receipt is used as current execution authority.
// GetPersonalEmailMailboxMutationReceipt отображает только закрытые setup receipt kinds и один typed result; историческая receipt не используется как текущее полномочие исполнения.
func (server *Server) GetPersonalEmailMailboxMutationReceipt(ctx context.Context, request *cp.GetPersonalEmailMailboxMutationReceiptRequest) (*cp.GetPersonalEmailMailboxMutationReceiptResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_GetPersonalEmailMailboxMutationReceipt_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, nil) {
		return nil, transportError(errs.ErrInvalid)
	}
	name, ok := cp.PersonalEmailMailboxMutationOperation_name[int32(request.GetOperation())]
	if !ok || request.GetOperation() == cp.PersonalEmailMailboxMutationOperation_PERSONAL_EMAIL_MAILBOX_MUTATION_OPERATION_UNSPECIFIED {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.GetPersonalEmailMailboxMutationReceipt(ctx, p, strings.TrimPrefix(name, "PERSONAL_EMAIL_MAILBOX_MUTATION_OPERATION_"), request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	receipt := &cp.PersonalEmailMailboxMutationReceipt{Operation: request.GetOperation()}
	if result.Mailbox != nil && result.Ownership == nil {
		item := result.Mailbox
		receipt.Result = &cp.PersonalEmailMailboxMutationReceipt_Mailbox{Mailbox: &cp.MyEmailMailbox{Ownership: castEmailMailboxOwnership(item.Ownership), Name: item.Name, Enabled: item.Enabled, Available: item.Available}}
	} else if result.Ownership != nil && result.Mailbox == nil {
		receipt.Result = &cp.PersonalEmailMailboxMutationReceipt_Ownership{Ownership: castEmailMailboxOwnership(*result.Ownership)}
	} else {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.GetPersonalEmailMailboxMutationReceiptResponse{Receipt: receipt}, nil
}

// CreatePersonalEmailMailboxConnection delegates fixed self setup to the owner transaction; it cannot accept project, agent, credential owner, adapter, role or access-scope selectors.
// CreatePersonalEmailMailboxConnection передаёт фиксированную собственную настройку owner-транзакции; не принимает selectors project, agent, credential owner, adapter, role либо access scope.
func (server *Server) CreatePersonalEmailMailboxConnection(ctx context.Context, request *cp.CreatePersonalEmailMailboxConnectionRequest) (*cp.CreatePersonalEmailMailboxConnectionResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_CreatePersonalEmailMailboxConnection_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.CreatePersonalEmailMailboxConnection, Principal: p, Mutation: mutation(request.GetMutation()),
		Payload: command.PersonalEmailMailboxConnectionInput{Name: request.GetName(), Sender: request.GetSender()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.EmailMailboxOwnership == nil || result.Connection == nil || result.Connection.Ref != result.EmailMailboxOwnership.ConnectionRef {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.CreatePersonalEmailMailboxConnectionResponse{Mailbox: &cp.MyEmailMailbox{Ownership: castEmailMailboxOwnership(*result.EmailMailboxOwnership), Name: result.Connection.Name, Enabled: result.Connection.Enabled, Available: false}}, nil
}

// castEmailMailboxOwnership projects only safe self-setup metadata; personal subject IDs and secret descriptors have no representation in the response.
// castEmailMailboxOwnership проецирует только безопасные metadata собственной настройки; личные subject IDs и secret descriptors не имеют представления в ответе.
func castEmailMailboxOwnership(item entity.EmailMailboxOwnershipView) *cp.EmailMailboxOwnership {
	return &cp.EmailMailboxOwnership{ConnectionRef: item.ConnectionRef, ConnectionVersion: item.ConnectionVersion,
		Policy:     cp.IntegrationCredentialPolicy(cp.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(item.Policy)]),
		State:      cp.EmailMailboxOwnershipState(cp.EmailMailboxOwnershipState_value["EMAIL_MAILBOX_OWNERSHIP_STATE_"+item.State]),
		Generation: item.Generation, ServiceSubjectRef: item.ServiceSubjectRef, OwnerEligible: item.OwnerEligible}
}

// ListMyEmailMailboxes uses the verified actor and owner-filtered cursor page, without accepting a tenant or personal owner selector from the request.
// ListMyEmailMailboxes использует проверенного actor и страницу с owner-фильтром без выбора tenant либо личного owner из запроса.
func (server *Server) ListMyEmailMailboxes(ctx context.Context, request *cp.ListMyEmailMailboxesRequest) (*cp.ListMyEmailMailboxesResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_ListMyEmailMailboxes_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, nil) {
		return nil, transportError(errs.ErrInvalid)
	}
	items, next, err := server.service.ListMyEmailMailboxes(ctx, p, request.GetQuery(), query.Page{Size: request.GetPageSize(), Token: request.GetPageToken()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListMyEmailMailboxesResponse{Items: make([]*cp.MyEmailMailbox, 0, len(items)), NextPageToken: next}
	for _, item := range items {
		response.Items = append(response.Items, &cp.MyEmailMailbox{Ownership: castEmailMailboxOwnership(item.Ownership), Name: item.Name, Enabled: item.Enabled, Available: item.Available})
	}
	return response, nil
}

// GetEmailMailboxOwnership returns the dedicated current self view after owner authorization; a generic credential NONE profile is not interpreted as permission or missing token metadata.
// GetEmailMailboxOwnership выдаёт отдельное текущее собственное представление после проверки owner; generic credential NONE не трактуется как разрешение либо отсутствие token metadata.
func (server *Server) GetEmailMailboxOwnership(ctx context.Context, request *cp.GetEmailMailboxOwnershipRequest) (*cp.GetEmailMailboxOwnershipResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_GetEmailMailboxOwnership_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, nil) {
		return nil, transportError(errs.ErrInvalid)
	}
	item, err := server.service.GetEmailMailboxOwnership(ctx, p, request.GetConnectionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetEmailMailboxOwnershipResponse{Ownership: castEmailMailboxOwnership(item)}, nil
}

// ConfigureEmailMailboxOwnership validates the closed policy enum before issuing the specialized owner command; USER ownership is derived from transport rather than payload.
// ConfigureEmailMailboxOwnership проверяет закрытый enum policy до специализированной owner команды; владение USER выводится из transport, а не payload.
func (server *Server) ConfigureEmailMailboxOwnership(ctx context.Context, request *cp.ConfigureEmailMailboxOwnershipRequest) (*cp.ConfigureEmailMailboxOwnershipResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_ConfigureEmailMailboxOwnership_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	name, ok := cp.IntegrationCredentialPolicy_name[int32(request.GetPolicy())]
	if !ok || request.GetPolicy() == cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_UNSPECIFIED {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.ConfigureEmailMailboxOwnership, Principal: p, Mutation: mutation(request.GetMutation()),
		Payload: command.EmailMailboxOwnershipInput{ConnectionRef: request.GetConnectionRef(), Policy: entity.IntegrationCredentialPolicy(strings.TrimPrefix(name, "INTEGRATION_CREDENTIAL_POLICY_")), ServiceSubjectRef: request.GetServiceSubjectRef()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.EmailMailboxOwnership == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.ConfigureEmailMailboxOwnershipResponse{Ownership: castEmailMailboxOwnership(*result.EmailMailboxOwnership)}, nil
}

// RevokeEmailMailboxOwnership fences new admissions through the existing owner transaction and leaves uncertain remote effects and historical receipts intact.
// RevokeEmailMailboxOwnership ограждает новые допуски в существующей owner-транзакции, сохраняя неопределённые удалённые эффекты и исторические receipts.
func (server *Server) RevokeEmailMailboxOwnership(ctx context.Context, request *cp.RevokeEmailMailboxOwnershipRequest) (*cp.RevokeEmailMailboxOwnershipResponse, error) {
	p, err := principal(ctx, cp.PersonalEmailMailboxService_RevokeEmailMailboxOwnership_FullMethodName)
	if err != nil {
		return nil, err
	}
	if !knownPersonalEmailRequest(request, request.GetMutation()) {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.RevokeEmailMailboxOwnership, Principal: p, Mutation: mutation(request.GetMutation()), Payload: command.EmailMailboxOwnershipInput{ConnectionRef: request.GetConnectionRef()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.EmailMailboxOwnership == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.RevokeEmailMailboxOwnershipResponse{Ownership: castEmailMailboxOwnership(*result.EmailMailboxOwnership)}, nil
}
