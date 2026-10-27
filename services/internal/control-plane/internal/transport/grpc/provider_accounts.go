package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// CreateProviderAccount passes definition and display name through the dedicated create command
// and projects its account result.
// CreateProviderAccount передаёт определение и отображаемое имя отдельной команде создания и
// преобразует аккаунт из результата.
func (server *Server) CreateProviderAccount(
	ctx context.Context,
	request *controlplanev1.CreateProviderAccountRequest,
) (*controlplanev1.CreateProviderAccountResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateProviderAccount_FullMethodName, command.CreateProviderAccount, request.GetMutation(), command.ProviderAccountInput{DefinitionKey: request.GetDefinitionKey(), Name: request.GetName()}, func(result command.Result) *controlplanev1.CreateProviderAccountResponse {
		return &controlplanev1.CreateProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}

// StartProviderAccountDeviceAuthorization uses the exact method principal and supplied mutation to
// start device authorization through the owner service.
// StartProviderAccountDeviceAuthorization использует principal точного метода и переданную
// mutation для начала device authorization через сервис-владелец.
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

// AuthorizeProviderAccountAPIKey copies the incoming key into a temporary byte slice for the
// authorized owner call and clears that copy on return.
// AuthorizeProviderAccountAPIKey копирует входной ключ во временный срез байтов для
// авторизованного вызова владельца и очищает эту копию при возврате.
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

// RefreshProviderAccountAuthorization passes the account locator and mutation to the authorized
// refresh method without adding transport-owned retries.
// RefreshProviderAccountAuthorization передаёт ссылку аккаунта и mutation авторизованному методу
// обновления без повторов силами transport.
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

// VerifyProviderAccountDeviceAuthorization executes the dedicated device-authorization
// verification command for the supplied account locator.
// VerifyProviderAccountDeviceAuthorization выполняет отдельную команду проверки device
// authorization по переданной ссылке аккаунта.
func (server *Server) VerifyProviderAccountDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.VerifyProviderAccountDeviceAuthorizationRequest,
) (*controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_VerifyProviderAccountDeviceAuthorization_FullMethodName, command.VerifyProviderAuthorization, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef()}, func(result command.Result) *controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse {
		return &controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}

// ReauthorizeProviderAccountDeviceCode uses its distinct RPC principal to call the owner's
// device-authorization start method with the supplied account and mutation.
// ReauthorizeProviderAccountDeviceCode использует principal отдельного RPC для вызова начала
// device authorization владельца с переданными аккаунтом и mutation.
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

// RevokeProviderAccount passes the account locator through the dedicated revoke command and
// projects the returned account state.
// RevokeProviderAccount передаёт ссылку аккаунта отдельной команде отзыва и преобразует
// возвращённое состояние аккаунта.
func (server *Server) RevokeProviderAccount(
	ctx context.Context,
	request *controlplanev1.RevokeProviderAccountRequest,
) (*controlplanev1.RevokeProviderAccountResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RevokeProviderAccount_FullMethodName, command.RevokeProviderAccount, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef()}, func(result command.Result) *controlplanev1.RevokeProviderAccountResponse {
		return &controlplanev1.RevokeProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}

// DeleteProviderAccount passes the account locator through the dedicated delete command and
// returns the owner's account projection.
// DeleteProviderAccount передаёт ссылку аккаунта отдельной команде удаления и возвращает
// представление аккаунта владельца.
func (server *Server) DeleteProviderAccount(
	ctx context.Context,
	request *controlplanev1.DeleteProviderAccountRequest,
) (*controlplanev1.DeleteProviderAccountResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_DeleteProviderAccount_FullMethodName, command.DeleteProviderAccount, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef()}, func(result command.Result) *controlplanev1.DeleteProviderAccountResponse {
		return &controlplanev1.DeleteProviderAccountResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}

// SetProviderAccountEnabled passes the requested enabled flag and account locator through the
// dedicated command, leaving eligibility to the owner.
// SetProviderAccountEnabled передаёт запрошенный enabled и ссылку аккаунта отдельной команде,
// оставляя проверку доступности владельцу.
func (server *Server) SetProviderAccountEnabled(
	ctx context.Context,
	request *controlplanev1.SetProviderAccountEnabledRequest,
) (*controlplanev1.SetProviderAccountEnabledResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_SetProviderAccountEnabled_FullMethodName, command.SetProviderAccountEnabled, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef(), Enabled: request.GetEnabled()}, func(result command.Result) *controlplanev1.SetProviderAccountEnabledResponse {
		return &controlplanev1.SetProviderAccountEnabledResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}

// SetProviderAccountConcurrency passes the proposed concurrency limit and account locator through
// the dedicated command without validating domain limits here.
// SetProviderAccountConcurrency передаёт предлагаемый лимит параллельности и ссылку аккаунта
// отдельной команде без проверки доменных лимитов в этом методе.
func (server *Server) SetProviderAccountConcurrency(
	ctx context.Context,
	request *controlplanev1.SetProviderAccountConcurrencyRequest,
) (*controlplanev1.SetProviderAccountConcurrencyResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_SetProviderAccountConcurrency_FullMethodName, command.SetProviderAccountConcurrency, request.GetMutation(), command.ProviderAccountInput{AccountRef: request.GetAccountRef(), MaximumConcurrentExecutions: request.GetMaximumConcurrentExecutions()}, func(result command.Result) *controlplanev1.SetProviderAccountConcurrencyResponse {
		return &controlplanev1.SetProviderAccountConcurrencyResponse{Account: castProviderAccount(*result.ProviderAccount)}
	})
}
