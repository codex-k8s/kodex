package grpc

import (
	"context"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) CheckProviderCredentialMaterializerReadiness(
	ctx context.Context,
	_ *controlplanev1.CheckProviderCredentialMaterializerReadinessRequest,
) (*controlplanev1.CheckProviderCredentialMaterializerReadinessResponse, error) {
	if server.providerCredentials == nil || server.providerCredentials.Check(ctx) != nil {
		return &controlplanev1.CheckProviderCredentialMaterializerReadinessResponse{Ready: false},
			status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	return &controlplanev1.CheckProviderCredentialMaterializerReadinessResponse{Ready: true}, nil
}

func (server *Server) StartDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	result, err := server.providerCredentials.StartDeviceAuthorization(ctx, request.GetAttemptRef(), request.GetAccountRef())
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationResponse{
		MaterializerAttemptRef:             result.MaterializerAttemptRef,
		VerificationUri:                    result.VerificationURI,
		UserCode:                           result.UserCode,
		ExpiresAt:                          timestamppb.New(result.ExpiresAt),
		MaterializerAttemptUid:             result.MaterializerAttemptUID,
		MaterializerAttemptResourceVersion: result.MaterializerAttemptVersion,
	}, nil
}

func (server *Server) ObserveDeviceAuthorization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	result, err := server.providerCredentials.ObserveDeviceAuthorization(ctx, request.GetMaterializerAttemptRef())
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
		State:                 providerAuthorizationState(result.State),
		Credential:            providerCredentialDescriptor(result.Credential),
		ExternalAccountMasked: result.ExternalAccountMasked,
		SafeFailureCode:       result.SafeFailureCode,
	}, nil
}

func (server *Server) MaterializeAPIKey(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	apiKey := append([]byte(nil), request.GetApiKey()...)
	defer clear(apiKey)
	credential, masked, err := server.providerCredentials.MaterializeAPIKey(ctx, request.GetAttemptRef(), request.GetAccountRef(), apiKey)
	if err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyResponse{
		Credential: providerCredentialDescriptor(&credential), ExternalAccountMasked: masked,
	}, nil
}

func (server *Server) DiscardProviderCredentialMaterialization(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	target := providercredential.DiscardMaterialization{
		AttemptRef: request.GetAttemptRef(), AccountRef: request.GetAccountRef(),
		MaterializerAttemptRef:     request.GetMaterializerAttemptRef(),
		MaterializerAttemptUID:     request.GetMaterializerAttemptUid(),
		MaterializerAttemptVersion: request.GetMaterializerAttemptResourceVersion(),
	}
	if credential := request.GetCredential(); credential != nil {
		target.Credential = &kubernetesstore.ProviderCredentialDescriptor{
			SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(),
			SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256(),
		}
	}
	if err := server.providerCredentials.Discard(ctx, target); err != nil {
		return nil, providerCredentialError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationResponse{Discarded: true}, nil
}

func (server *Server) CleanupProviderCredential(
	ctx context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest,
) (*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse, error) {
	if server.providerCredentials == nil {
		return nil, status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
	descriptor := kubernetesstore.ProviderCredentialDescriptor{}
	if credential := request.GetCredential(); credential != nil {
		descriptor = kubernetesstore.ProviderCredentialDescriptor{
			SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(),
			SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256(),
		}
	}
	receipt, err := server.providerCredentials.CleanupProviderCredential(
		ctx,
		request.GetTaskRef(),
		request.GetAccountRef(),
		request.GetLeaseGeneration(),
		descriptor,
	)
	if err != nil {
		return nil, providerCredentialCleanupError(err)
	}
	return &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
		TerminalReceipt: receipt,
	}, nil
}

func providerAuthorizationState(state string) controlplanev1.ProviderAuthorizationState {
	switch state {
	case "PENDING":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING
	case "AUTHORIZED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED
	case "EXPIRED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED
	case "FAILED":
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED
	default:
		return controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_UNSPECIFIED
	}
}

func providerCredentialDescriptor(value *kubernetesstore.ProviderCredentialDescriptor) *controlplanev1.ProviderCredentialDescriptor {
	if value == nil {
		return nil
	}
	return &controlplanev1.ProviderCredentialDescriptor{
		SecretName: value.SecretName, SecretUid: value.SecretUID,
		SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

func providerCredentialError(err error) error {
	switch {
	case errors.Is(err, providercredential.ErrInvalidInput),
		errors.Is(err, kubernetesstore.ErrProviderCredentialInputInvalid):
		return status.Error(codes.InvalidArgument, "provider credential materializer input is invalid")
	case errors.Is(err, providercredential.ErrNotFound), errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound):
		return status.Error(codes.NotFound, "provider authorization attempt is not found")
	case errors.Is(err, providercredential.ErrConflict),
		errors.Is(err, kubernetesstore.ErrProviderAttemptConflict),
		errors.Is(err, kubernetesstore.ErrProviderCredentialConflict):
		return status.Error(codes.Aborted, "provider credential materialization conflicts with stored state")
	default:
		return status.Error(codes.Unavailable, "provider credential materializer is unavailable")
	}
}

func providerCredentialCleanupError(err error) error {
	switch {
	case errors.Is(err, providercredential.ErrInvalidInput),
		errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupInvalid):
		return status.Error(codes.InvalidArgument, "provider credential cleanup input is invalid")
	case errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupConflict):
		return status.Error(codes.FailedPrecondition, "provider credential cleanup conflicts with stored state")
	default:
		return status.Error(codes.Unavailable, "provider credential cleanup is unavailable")
	}
}
