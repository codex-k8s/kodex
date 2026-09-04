package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) CreateProviderAccount(
	ctx context.Context,
	request *controlplanev1.CreateProviderAccountRequest,
) (*controlplanev1.CreateProviderAccountResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateProviderAccount_FullMethodName,
		command.CreateProviderAccount, request.GetMutation(), command.ProviderAccountInput{
			DefinitionKey: request.GetDefinitionKey(), Name: request.GetName(),
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}, nil
}

func (server *Server) StartProviderAccountDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.StartProviderAccountDeviceAuthorizationRequest,
) (*controlplanev1.StartProviderAccountDeviceAuthorizationResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_StartProviderAccountDeviceAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	account, err := server.service.StartProviderAccountDeviceAuthorization(ctx, principal, mutation(request.GetMutation()), request.GetAccountRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.StartProviderAccountDeviceAuthorizationResponse{Account: castProviderAccount(account)}, nil
}

func (server *Server) AuthorizeProviderAccountAPIKey(
	ctx context.Context,
	request *controlplanev1.AuthorizeProviderAccountAPIKeyRequest,
) (*controlplanev1.AuthorizeProviderAccountAPIKeyResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_AuthorizeProviderAccountAPIKey_FullMethodName)
	if err != nil {
		return nil, err
	}
	apiKey := append([]byte(nil), request.GetApiKey()...)
	defer clear(apiKey)
	account, err := server.service.AuthorizeProviderAccountAPIKey(ctx, principal, mutation(request.GetMutation()), request.GetAccountRef(), apiKey)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.AuthorizeProviderAccountAPIKeyResponse{Account: castProviderAccount(account)}, nil
}

func (server *Server) RefreshProviderAccountAuthorization(
	ctx context.Context,
	request *controlplanev1.RefreshProviderAccountAuthorizationRequest,
) (*controlplanev1.RefreshProviderAccountAuthorizationResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_RefreshProviderAccountAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	account, err := server.service.RefreshProviderAccountAuthorization(ctx, principal, mutation(request.GetMutation()), request.GetAccountRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RefreshProviderAccountAuthorizationResponse{Account: castProviderAccount(account)}, nil
}

func (server *Server) VerifyProviderAccountDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.VerifyProviderAccountDeviceAuthorizationRequest,
) (*controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_VerifyProviderAccountDeviceAuthorization_FullMethodName)
	if err != nil {
		return nil, err
	}
	account, err := server.service.RefreshProviderAccountAuthorization(ctx, principal, mutation(request.GetMutation()), request.GetAccountRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse{Account: castProviderAccount(account)}, nil
}

func (server *Server) ReauthorizeProviderAccountDeviceCode(
	ctx context.Context,
	request *controlplanev1.ReauthorizeProviderAccountDeviceCodeRequest,
) (*controlplanev1.ReauthorizeProviderAccountDeviceCodeResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_ReauthorizeProviderAccountDeviceCode_FullMethodName)
	if err != nil {
		return nil, err
	}
	account, err := server.service.StartProviderAccountDeviceAuthorization(ctx, principal, mutation(request.GetMutation()), request.GetAccountRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ReauthorizeProviderAccountDeviceCodeResponse{Account: castProviderAccount(account)}, nil
}

func (server *Server) RevokeProviderAccount(
	ctx context.Context,
	request *controlplanev1.RevokeProviderAccountRequest,
) (*controlplanev1.RevokeProviderAccountResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RevokeProviderAccount_FullMethodName,
		command.RevokeProviderAccount, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RevokeProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}, nil
}

func (server *Server) DeleteProviderAccount(
	ctx context.Context,
	request *controlplanev1.DeleteProviderAccountRequest,
) (*controlplanev1.DeleteProviderAccountResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteProviderAccount_FullMethodName,
		command.DeleteProviderAccount, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}, nil
}

func (server *Server) SetProviderAccountEnabled(
	ctx context.Context,
	request *controlplanev1.SetProviderAccountEnabledRequest,
) (*controlplanev1.SetProviderAccountEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetProviderAccountEnabled_FullMethodName,
		command.SetProviderAccountEnabled, request.GetMutation(), command.ProviderAccountInput{
			AccountRef: request.GetAccountRef(), Enabled: request.GetEnabled(),
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetProviderAccountEnabledResponse{Account: castProviderAccount(*result.ProviderAccount)}, nil
}
