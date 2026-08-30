// Package providercredentialclient адаптирует защищённый secret-broker gRPC
// client к доменному порту control-plane. Raw credentials не возвращаются.
package providercredentialclient

import (
	"context"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

type Client struct {
	client *controlplaneclient.Client
}

func New(client *controlplaneclient.Client) (*Client, error) {
	if client == nil || client.ProviderCredentials == nil {
		return nil, errors.New("provider credential materializer client is required")
	}
	return &Client{client: client}, nil
}

func (client *Client) Check(ctx context.Context) error {
	return client.client.CheckProviderCredentialMaterializer(ctx)
}

func (client *Client) StartDeviceAuthorization(
	ctx context.Context,
	attemptRef, accountRef string,
) (platformservice.ProviderDeviceAuthorizationMaterialization, error) {
	response, err := client.client.ProviderCredentials.StartDeviceAuthorization(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceStartDeviceAuthorizationRequest{
			AttemptRef: attemptRef, AccountRef: accountRef,
		})
	if err != nil {
		return platformservice.ProviderDeviceAuthorizationMaterialization{}, err
	}
	if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil {
		return platformservice.ProviderDeviceAuthorizationMaterialization{}, errors.New("provider device authorization expiry is invalid")
	}
	return platformservice.ProviderDeviceAuthorizationMaterialization{
		MaterializerAttemptRef: response.GetMaterializerAttemptRef(), VerificationURI: response.GetVerificationUri(),
		UserCode: response.GetUserCode(), ExpiresAt: response.GetExpiresAt().AsTime(),
		MaterializerAttemptUID:     response.GetMaterializerAttemptUid(),
		MaterializerAttemptVersion: response.GetMaterializerAttemptResourceVersion(),
	}, nil
}

func (client *Client) ObserveDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef string,
) (platformservice.ProviderAuthorizationObservation, error) {
	response, err := client.client.ProviderCredentials.ObserveDeviceAuthorization(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest{
			MaterializerAttemptRef: materializerAttemptRef,
		})
	if err != nil {
		return platformservice.ProviderAuthorizationObservation{}, err
	}
	return platformservice.ProviderAuthorizationObservation{
		State: providerAuthorizationState(response.GetState()), Credential: credentialDescriptor(response.GetCredential()),
		ExternalAccountMasked: response.GetExternalAccountMasked(), SafeFailureCode: response.GetSafeFailureCode(),
	}, nil
}

func (client *Client) MaterializeAPIKey(
	ctx context.Context,
	attemptRef, accountRef string,
	apiKey []byte,
) (entity.ProviderCredentialDescriptor, string, error) {
	response, err := client.client.ProviderCredentials.MaterializeAPIKey(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceMaterializeAPIKeyRequest{
			AttemptRef: attemptRef, AccountRef: accountRef, ApiKey: apiKey,
		})
	if err != nil {
		return entity.ProviderCredentialDescriptor{}, "", err
	}
	descriptor := credentialDescriptor(response.GetCredential())
	if descriptor == nil {
		return entity.ProviderCredentialDescriptor{}, "", errors.New("provider credential descriptor is missing")
	}
	return *descriptor, response.GetExternalAccountMasked(), nil
}

func (client *Client) Discard(ctx context.Context, target platformservice.ProviderMaterializationDiscard) error {
	request := &controlplanev1.ProviderCredentialMaterializerServiceDiscardMaterializationRequest{
		AttemptRef: target.AttemptRef, AccountRef: target.AccountRef,
		MaterializerAttemptRef:             target.MaterializerAttemptRef,
		MaterializerAttemptUid:             target.MaterializerAttemptUID,
		MaterializerAttemptResourceVersion: target.MaterializerAttemptVersion,
	}
	if target.Credential != nil {
		request.Credential = &controlplanev1.ProviderCredentialDescriptor{
			SecretName: target.Credential.SecretName, SecretUid: target.Credential.SecretUID,
			SecretResourceVersion: target.Credential.SecretResourceVersion,
			ContentSha256:         target.Credential.ContentSHA256,
		}
	}
	response, err := client.client.ProviderCredentials.DiscardProviderCredentialMaterialization(ctx, request)
	if err != nil {
		return err
	}
	if !response.GetDiscarded() {
		return errors.New("provider credential materialization was not discarded")
	}
	return nil
}

func credentialDescriptor(value *controlplanev1.ProviderCredentialDescriptor) *entity.ProviderCredentialDescriptor {
	if value == nil {
		return nil
	}
	return &entity.ProviderCredentialDescriptor{
		SecretName: value.GetSecretName(), SecretUID: value.GetSecretUid(),
		SecretResourceVersion: value.GetSecretResourceVersion(), ContentSHA256: value.GetContentSha256(),
	}
}

func providerAuthorizationState(value controlplanev1.ProviderAuthorizationState) string {
	switch value {
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING:
		return "PENDING"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_AUTHORIZED:
		return "AUTHORIZED"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_EXPIRED:
		return "EXPIRED"
	case controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_FAILED:
		return "FAILED"
	default:
		return ""
	}
}
