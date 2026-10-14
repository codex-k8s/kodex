package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const invalidCredentialRenewal = "provider credential renewal binding is invalid"

func credentialRenewalBinding(task repository.ProviderCredentialRenewalTask) (*cp.ProviderCredentialRenewalBinding, error) {
	if !validProviderCredentialRef(task.Ref, "pcrt_") || !validProviderCredentialRef(task.AccountRef, "pacc_") ||
		!validProviderCredentialRef(task.Fence, "prf_") || !validBoundedSafeText(task.CredentialRef, 96) ||
		!validBoundedSafeText(task.ClaimantID, 128) || !validProviderCredentialDescriptor(task.Previous) || task.ExpiresAt.IsZero() {
		return nil, errors.New(invalidCredentialRenewal)
	}
	for _, version := range []int64{task.AccountVersion, task.CredentialRevision, task.Generation} {
		if version < 1 || version > 9007199254740991 {
			return nil, errors.New(invalidCredentialRenewal)
		}
	}
	expires := timestamppb.New(task.ExpiresAt)
	if expires.CheckValid() != nil {
		return nil, errors.New(invalidCredentialRenewal)
	}
	return &cp.ProviderCredentialRenewalBinding{TaskRef: task.Ref, AccountRef: task.AccountRef, AccountVersion: task.AccountVersion,
		CredentialRevisionRef: task.CredentialRef, CredentialRevision: task.CredentialRevision, ClaimantId: task.ClaimantID, ClaimGeneration: task.Generation,
		Fence: task.Fence, ExpiresAt: expires, PreviousCredential: &cp.ProviderCredentialDescriptor{SecretName: task.Previous.SecretName,
			SecretUid: task.Previous.SecretUID, SecretResourceVersion: task.Previous.SecretResourceVersion, ContentSha256: task.Previous.ContentSHA256}}, nil
}

func (client *Client) CredentialRenewalRequestDigests(task repository.ProviderCredentialRenewalTask) (string, string, error) {
	binding, err := credentialRenewalBinding(task)
	if err != nil {
		return "", "", err
	}
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cp.RenewManagedProviderCredentialRequest{Binding: binding})
	if err != nil {
		return "", "", errors.New(invalidCredentialRenewal)
	}
	writer := sha256.Sum256(raw)
	writerDigest := hex.EncodeToString(writer[:])
	raw, err = (proto.MarshalOptions{Deterministic: true}).Marshal(&cp.AuthorizeProviderCredentialRenewalRequest{Binding: binding, RenewalRequestSha256: writerDigest})
	if err != nil {
		return "", "", errors.New(invalidCredentialRenewal)
	}
	owner := sha256.Sum256(raw)
	return writerDigest, hex.EncodeToString(owner[:]), nil
}

func (client *Client) RenewManagedProviderCredential(ctx context.Context, task repository.ProviderCredentialRenewalTask) (repository.ProviderCredentialRenewalResult, error) {
	binding, err := client.validRenewalTask(task)
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	response, err := client.client.ProviderCredentials.RenewManagedProviderCredential(ctx, &cp.RenewManagedProviderCredentialRequest{Binding: binding})
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return repository.ProviderCredentialRenewalResult{}, errors.New(invalidCredentialRenewal)
	}
	return credentialRenewalResult(response.GetState(), response.GetCredential(), response.GetMaterializerReceiptUid(), response.GetMaterializerReceiptResourceVersion())
}

func (client *Client) ObserveManagedProviderCredentialRenewal(ctx context.Context, task repository.ProviderCredentialRenewalTask) (repository.ProviderCredentialRenewalResult, error) {
	binding, err := client.validRenewalTask(task)
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	response, err := client.client.ProviderCredentials.ObserveManagedProviderCredentialRenewal(ctx, &cp.ObserveManagedProviderCredentialRenewalRequest{Binding: binding})
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return repository.ProviderCredentialRenewalResult{}, errors.New(invalidCredentialRenewal)
	}
	result, err := credentialRenewalResult(response.GetState(), response.GetCredential(), response.GetMaterializerReceiptUid(), response.GetMaterializerReceiptResourceVersion())
	result.Recovered = true
	return result, err
}

func (client *Client) validRenewalTask(task repository.ProviderCredentialRenewalTask) (*cp.ProviderCredentialRenewalBinding, error) {
	writer, owner, err := client.CredentialRenewalRequestDigests(task)
	if err != nil || writer != task.RequestDigest || owner != task.OwnerRequestDigest {
		return nil, errors.New(invalidCredentialRenewal)
	}
	return credentialRenewalBinding(task)
}

func credentialRenewalResult(state cp.ProviderCredentialRenewalState, credential *cp.ProviderCredentialDescriptor, uid, version string) (repository.ProviderCredentialRenewalResult, error) {
	result := repository.ProviderCredentialRenewalResult{ReceiptUID: uid, ReceiptVersion: version}
	switch state {
	case cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_SUCCEEDED:
		if credential == nil || len(credential.ProtoReflect().GetUnknown()) != 0 {
			return result, errors.New(invalidCredentialRenewal)
		}
		value := entity.ProviderCredentialDescriptor{SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(), SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256()}
		if !validProviderCredentialDescriptor(value) || !validProviderCredentialDescriptor(entity.ProviderCredentialDescriptor{SecretName: value.SecretName, SecretUID: uid, SecretResourceVersion: version, ContentSHA256: value.ContentSHA256}) {
			return result, errors.New(invalidCredentialRenewal)
		}
		result.State, result.Credential = "SUCCEEDED", &value
	case cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_RUNNING, cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_UNKNOWN_OUTCOME:
		if credential != nil {
			return result, errors.New(invalidCredentialRenewal)
		}
		result.State = "UNKNOWN_OUTCOME"
	default:
		return result, errors.New(invalidCredentialRenewal)
	}
	return result, nil
}
