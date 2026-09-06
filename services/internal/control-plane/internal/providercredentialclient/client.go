// Package providercredentialclient адаптирует защищённый secret-broker gRPC
// client к доменному порту control-plane. Raw credentials не возвращаются.
package providercredentialclient

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
)

const (
	maximumProviderCredentialRefBytes             = 96
	maximumProviderCredentialResourceVersionBytes = 128
	maximumProviderCredentialTerminalReceiptBytes = 512
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
			Mode:                   controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_POLL,
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

func (client *Client) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	credential entity.ProviderCredentialDescriptor,
	recovery ...*entity.ProviderCleanupRecoveryIdentity,
) (entity.ProviderAuthorizationCleanupResult, error) {
	var result entity.ProviderAuthorizationCleanupResult
	if !validProviderCredentialRef(taskRef, "pcct_") ||
		!validProviderCredentialRef(accountRef, "pacc_") ||
		leaseGeneration < 1 || !validProviderCredentialDescriptor(credential) {
		return result, errors.New("provider credential cleanup request is invalid")
	}
	request := &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
		TaskRef: taskRef, AccountRef: accountRef, LeaseGeneration: leaseGeneration,
		TargetKind: controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL,
		Credential: &controlplanev1.ProviderCredentialDescriptor{
			SecretName: credential.SecretName, SecretUid: credential.SecretUID,
			SecretResourceVersion: credential.SecretResourceVersion,
			ContentSha256:         credential.ContentSHA256,
		},
	}
	if len(recovery) > 1 {
		return result, errors.New("provider cleanup recovery identity is invalid")
	}
	if len(recovery) == 1 {
		var err error
		request.RecoveryIdentity, err = cleanupRecoveryIdentity(recovery[0])
		if err != nil {
			return result, err
		}
	}
	response, err := client.client.ProviderCredentials.CleanupProviderCredential(ctx, request)
	if err != nil {
		return result, err
	}
	receipt := response.GetTerminalReceipt()
	result.TerminalReceipt, result.ProducedCredential = receipt, credentialDescriptor(response.GetProducedCredential())
	if !validBoundedSafeText(receipt, maximumProviderCredentialTerminalReceiptBytes) ||
		(result.ProducedCredential != nil && !validProviderCredentialDescriptor(*result.ProducedCredential)) {
		return entity.ProviderAuthorizationCleanupResult{}, errors.New("provider credential cleanup terminal receipt is invalid")
	}
	return result, nil
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

func validProviderCredentialRef(value, prefix string) bool {
	if len(value) < len(prefix)+8 || len(value) > maximumProviderCredentialRefBytes || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validProviderCredentialDescriptor(value entity.ProviderCredentialDescriptor) bool {
	parsedUID, err := uuid.Parse(value.SecretUID)
	return validDNSLabel(value.SecretName) && err == nil && parsedUID.String() == value.SecretUID &&
		validBoundedSafeText(value.SecretResourceVersion, maximumProviderCredentialResourceVersionBytes) &&
		validLowerHexSHA256(value.ContentSHA256)
}

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		if ((character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-') ||
			character == '-' && (index == 0 || index == len(value)-1) {
			return false
		}
	}
	return true
}

func validLowerHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validBoundedSafeText(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
