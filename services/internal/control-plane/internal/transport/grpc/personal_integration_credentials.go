package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// personalCredentialResultUnavailable is a fixed diagnostic shared by self mutation responses.
// personalCredentialResultUnavailable — фиксированная диагностика ответов собственных изменений.
const personalCredentialResultUnavailable = "personal integration credential result unavailable"

// ListPersonalIntegrationConnections uses only the method-bound principal, typed page/search and
// sanitized projections. An absent self binding stays absent in the response; no GET performs a write.
// ListPersonalIntegrationConnections использует только method-bound principal, typed page/search и
// безопасные проекции. Отсутствующая собственная привязка остаётся отсутствующей; GET не пишет данные.
func (server *Server) ListPersonalIntegrationConnections(ctx context.Context, request *controlplanev1.ListPersonalIntegrationConnectionsRequest) (*controlplanev1.ListPersonalIntegrationConnectionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_ListPersonalIntegrationConnections_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListPersonalIntegrationConnections(ctx, p, query.PersonalIntegrationConnectionFilter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	result := &controlplanev1.ListPersonalIntegrationConnectionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	now := time.Now().UTC()
	for _, item := range items {
		policy, known := controlplanev1.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(item.Policy)]
		if !known || policy == 0 || item.Ref == "" || item.Version < 1 || item.Name == "" || item.DefinitionKey == "" {
			return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
		}
		view := &controlplanev1.PersonalIntegrationConnection{ConnectionRef: item.Ref, ConnectionVersion: item.Version,
			Name: item.Name, DefinitionKey: item.DefinitionKey, Policy: controlplanev1.IntegrationCredentialPolicy(policy), Enabled: item.Enabled, Available: item.Available}
		if item.Credential != nil {
			if item.Credential.ConnectionRef != item.Ref || item.Credential.ConnectionVersion != item.Version || item.Credential.CredentialPolicy != item.Policy {
				return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
			}
			view.Credential, err = castPersonalIntegrationCredential(*item.Credential, now)
			if err != nil {
				return nil, err
			}
		}
		result.Connections = append(result.Connections, view)
	}
	return result, nil
}

// SetIntegrationCredentialPolicy accepts only the closed enum and dedicated configuration command;
// the owner checks current management authority and inactive dependencies before changing selection.
// SetIntegrationCredentialPolicy принимает только закрытый enum и выделенную команду настройки;
// владелец проверяет текущие права управления и неактивные зависимости до смены выбора.
func (server *Server) SetIntegrationCredentialPolicy(ctx context.Context, request *controlplanev1.SetIntegrationCredentialPolicyRequest) (*controlplanev1.SetIntegrationCredentialPolicyResponse, error) {
	p, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_SetIntegrationCredentialPolicy_FullMethodName)
	if err != nil {
		return nil, err
	}
	var policy entity.IntegrationCredentialPolicy
	switch request.GetPolicy() {
	case controlplanev1.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED:
		policy = entity.IntegrationCredentialProjectShared
	case controlplanev1.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED:
		policy = entity.IntegrationCredentialInitiatorRequired
	default:
		return nil, status.Error(codes.InvalidArgument, "integration credential policy is invalid")
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: p,
		Mutation: mutation(request.GetMutation()), Payload: command.IntegrationCredentialPolicyInput{ConnectionRef: request.GetConnectionRef(), Policy: policy}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.Connection == nil {
		return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
	}
	return &controlplanev1.SetIntegrationCredentialPolicyResponse{Connection: castConnection(*result.Connection)}, nil
}

// GetPersonalIntegrationCredential resolves only the method-bound authenticated user's binding
// and maps its metadata to a response that has no Secret descriptor fields.
// GetPersonalIntegrationCredential разрешает только привязку аутентифицированного пользователя
// exact method и преобразует metadata в ответ без полей Secret descriptor.
func (server *Server) GetPersonalIntegrationCredential(ctx context.Context, request *controlplanev1.GetPersonalIntegrationCredentialRequest) (*controlplanev1.GetPersonalIntegrationCredentialResponse, error) {
	principal, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_GetPersonalIntegrationCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.GetPersonalIntegrationCredential(ctx, principal, request.GetConnectionRef())
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castPersonalIntegrationCredential(binding, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &controlplanev1.GetPersonalIntegrationCredentialResponse{Credential: view}, nil
}

// CreatePersonalIntegrationCredential creates an empty self-owned binding; no owner selector or
// credential bytes are accepted and configuration OCC remains separate from binding OCC.
// CreatePersonalIntegrationCredential создаёт пустую собственную привязку; owner selector и
// credential bytes не принимаются, configuration OCC отделён от binding OCC.
func (server *Server) CreatePersonalIntegrationCredential(ctx context.Context, request *controlplanev1.CreatePersonalIntegrationCredentialRequest) (*controlplanev1.CreatePersonalIntegrationCredentialResponse, error) {
	principal, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_CreatePersonalIntegrationCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.CreatePersonalIntegrationCredential, Principal: principal,
		Mutation: mutation(request.GetMutation()), Payload: command.PersonalIntegrationCredentialInput{
			ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.PersonalIntegrationCredential == nil {
		return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
	}
	view, err := castPersonalIntegrationCredential(*result.PersonalIntegrationCredential, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreatePersonalIntegrationCredentialResponse{Credential: view}, nil
}

// ConfigurePersonalIntegrationCredential passes bytes only to the specialized materialization
// service, clears the received buffer on every outcome and returns safe metadata only.
// ConfigurePersonalIntegrationCredential передаёт bytes только специализированному materialization
// service, очищает принятый buffer при любом исходе и возвращает только безопасные metadata.
func (server *Server) ConfigurePersonalIntegrationCredential(ctx context.Context, request *controlplanev1.ConfigurePersonalIntegrationCredentialRequest) (*controlplanev1.ConfigurePersonalIntegrationCredentialResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "personal integration credential request required")
	}
	credential := request.GetCredentialValue()
	defer func() { clear(credential); request.CredentialValue = nil }()
	principal, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_ConfigurePersonalIntegrationCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.ConfigurePersonalIntegrationCredential(ctx, principal, mutation(request.GetMutation()),
		request.GetConnectionRef(), request.GetExpectedConnectionVersion(), credential)
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castPersonalIntegrationCredential(binding, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ConfigurePersonalIntegrationCredentialResponse{Credential: view}, nil
}

// RevokePersonalIntegrationCredential targets only the authenticated actor's Connection binding;
// a project administrator cannot select another user's descriptor through this request.
// RevokePersonalIntegrationCredential адресует только привязку Connection аутентифицированного actor;
// администратор проекта не выбирает чужой descriptor через этот запрос.
func (server *Server) RevokePersonalIntegrationCredential(ctx context.Context, request *controlplanev1.RevokePersonalIntegrationCredentialRequest) (*controlplanev1.RevokePersonalIntegrationCredentialResponse, error) {
	principal, err := principal(ctx, controlplanev1.PersonalIntegrationCredentialService_RevokePersonalIntegrationCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: principal,
		Mutation: mutation(request.GetMutation()), Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: request.GetConnectionRef()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.PersonalIntegrationCredential == nil {
		return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
	}
	view, err := castPersonalIntegrationCredential(*result.PersonalIntegrationCredential, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RevokePersonalIntegrationCredentialResponse{Credential: view}, nil
}

// castPersonalIntegrationCredential copies only browser-safe metadata and rejects unknown stored
// state/policy. Known expiry is projected as EXPIRED without rewriting immutable history or
// claiming readiness for unverified capabilities; a view never becomes an execution grant.
// castPersonalIntegrationCredential копирует только browser-safe metadata и отклоняет неизвестные
// stored state/policy. Известный срок даёт EXPIRED без изменения immutable history и без заявления
// готовности непроверенных capabilities; view никогда не становится execution grant.
func castPersonalIntegrationCredential(binding entity.PersonalIntegrationCredential, now time.Time) (*controlplanev1.PersonalIntegrationCredentialView, error) {
	invalid := func() (*controlplanev1.PersonalIntegrationCredentialView, error) {
		return nil, status.Error(codes.Unavailable, "personal integration credential metadata unavailable")
	}
	policy, policyKnown := controlplanev1.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(binding.CredentialPolicy)]
	state, stateKnown := controlplanev1.PersonalIntegrationCredentialState_value["PERSONAL_INTEGRATION_CREDENTIAL_STATE_"+binding.State]
	if binding.Ref == "" || binding.ConnectionRef == "" || binding.Version < 1 || binding.ConnectionVersion < 1 || now.IsZero() ||
		!policyKnown || policy == 0 || !stateKnown || state == 0 || binding.State == "EXPIRED" ||
		len(binding.VerifiedCapabilities) > 256 || len(binding.DeniedCapabilities) > 256 {
		return invalid()
	}
	if binding.Credential != nil && (!binding.Credential.ValidDescriptor() || binding.Credential.Ref == "" || binding.Credential.Revision < 1 || binding.Credential.CreatedAt.IsZero()) {
		return invalid()
	}
	if binding.State == "NOT_CONFIGURED" && binding.Credential != nil ||
		binding.State != "NOT_CONFIGURED" && binding.State != "REVOKED" && binding.Credential == nil {
		return invalid()
	}
	view := &controlplanev1.PersonalIntegrationCredentialView{ConnectionRef: binding.ConnectionRef, BindingRef: binding.Ref,
		Version: binding.Version, ConnectionVersion: binding.ConnectionVersion, Policy: controlplanev1.IntegrationCredentialPolicy(policy),
		State: controlplanev1.PersonalIntegrationCredentialState(state), CredentialConfigured: binding.Credential != nil,
		VerifiedCapabilityKeys: append([]string(nil), binding.VerifiedCapabilities...), DeniedCapabilityKeys: append([]string(nil), binding.DeniedCapabilities...)}
	if binding.ExpiresAt != nil {
		view.ExpiresAt = timestamppb.New(*binding.ExpiresAt)
		if view.ExpiresAt.CheckValid() != nil {
			return invalid()
		}
		if binding.State == "READY" && !binding.ExpiresAt.After(now) {
			view.State = controlplanev1.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_EXPIRED
		}
	}
	return view, nil
}
