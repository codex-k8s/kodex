package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const invalidModelCatalog = "provider model catalog binding is invalid"

func modelCatalogRequest(task platformrepo.ProviderModelCatalogTask) (*controlplanev1.ObserveProviderModelCatalogRequest, error) {
	method, ok := controlplanev1.ProviderAuthorizationMethod_value["PROVIDER_AUTHORIZATION_METHOD_"+task.AuthorizationMethod]
	if !ok || method == 0 || !validProviderCredentialRef(task.Ref, "mcattsk_") || !validProviderCredentialRef(task.AccountRef, "pacc_") || task.AccountVersion < 1 || task.AccountVersion > 9007199254740991 || task.CredentialRevision < 1 || task.CredentialRevision > 9007199254740991 || task.ClaimGeneration < 1 || task.ClaimGeneration > 9007199254740991 || !validBoundedSafeText(task.ClaimantID, 128) || !validBoundedSafeText(task.Fence, 128) || !validBoundedSafeText(task.CredentialRef, 96) || !validProviderCredentialDescriptor(task.Credential) || task.ExpiresAt.IsZero() {
		return nil, errors.New(invalidModelCatalog)
	}
	expires := timestamppb.New(task.ExpiresAt)
	if expires.CheckValid() != nil {
		return nil, errors.New(invalidModelCatalog)
	}
	return &controlplanev1.ObserveProviderModelCatalogRequest{
		TaskRef: task.Ref, ClaimantId: task.ClaimantID, ClaimGeneration: task.ClaimGeneration, Fence: task.Fence,
		AccountRef: task.AccountRef, AccountVersion: task.AccountVersion, CredentialRevisionRef: task.CredentialRef, CredentialRevision: task.CredentialRevision,
		Credential:          &controlplanev1.ProviderCredentialDescriptor{SecretName: task.Credential.SecretName, SecretUid: task.Credential.SecretUID, SecretResourceVersion: task.Credential.SecretResourceVersion, ContentSha256: task.Credential.ContentSHA256},
		AuthorizationMethod: controlplanev1.ProviderAuthorizationMethod(method), ExpiresAt: expires,
	}, nil
}

func (client *Client) ModelCatalogRequestDigest(task platformrepo.ProviderModelCatalogTask) (string, error) {
	request, err := modelCatalogRequest(task)
	if err != nil {
		return "", err
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (client *Client) ObserveProviderModelCatalog(ctx context.Context, task platformrepo.ProviderModelCatalogTask) (platformrepo.ProviderModelCatalogObservation, error) {
	request, err := modelCatalogRequest(task)
	if err != nil {
		return platformrepo.ProviderModelCatalogObservation{}, err
	}
	digest, err := client.ModelCatalogRequestDigest(task)
	if err != nil || digest != task.RequestDigest {
		return platformrepo.ProviderModelCatalogObservation{}, errors.New(invalidModelCatalog)
	}
	response, err := client.client.ProviderCredentials.ObserveProviderModelCatalog(ctx, request)
	if err != nil {
		return platformrepo.ProviderModelCatalogObservation{}, err
	}
	return modelCatalogObservation(task, response)
}

func modelCatalogObservation(task platformrepo.ProviderModelCatalogTask, response *controlplanev1.ObserveProviderModelCatalogResponse) (platformrepo.ProviderModelCatalogObservation, error) {
	invalid := func() (platformrepo.ProviderModelCatalogObservation, error) {
		return platformrepo.ProviderModelCatalogObservation{}, errors.New(invalidModelCatalog)
	}
	if response == nil || response.GetAccountRef() != task.AccountRef || response.GetCredentialRevisionRef() != task.CredentialRef || response.GetObservedAt() == nil || response.GetObservedAt().CheckValid() != nil || len(response.GetModels()) > 128 {
		return invalid()
	}
	result := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, ObservedAt: response.GetObservedAt().AsTime(), Models: []platformrepo.ProviderModelCatalogRecord{}}
	switch response.GetFailure() {
	case controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE:
		result.Failure = "NONE"
		if response.GetSource() == controlplanev1.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API && task.AuthorizationMethod == "API_KEY" {
			result.Source = "REMOTE_API"
		} else if response.GetSource() == controlplanev1.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX && task.AuthorizationMethod == "DEVICE_CODE" {
			result.Source = "REMOTE_CODEX"
		} else {
			return invalid()
		}
	case controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNAVAILABLE, controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNVERIFIED_SOURCE, controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_AUTHORIZATION_REJECTED:
		if response.GetSource() != controlplanev1.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_UNSPECIFIED || len(response.GetModels()) != 0 {
			return invalid()
		}
		result.Failure = strings.TrimPrefix(response.GetFailure().String(), "PROVIDER_MODEL_CATALOG_FAILURE_")
	default:
		return invalid()
	}
	seen := map[string]bool{}
	for _, model := range response.GetModels() {
		if model == nil || !validBoundedSafeText(model.GetId(), 200) || seen[model.GetId()] || len(model.GetReasoningEfforts()) > 16 {
			return invalid()
		}
		seen[model.GetId()] = true
		efforts := append([]string{}, model.GetReasoningEfforts()...)
		slices.Sort(efforts)
		for index, effort := range efforts {
			if runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil || index > 0 && efforts[index-1] == effort {
				return invalid()
			}
		}
		if len(efforts) == 0 && model.GetDefaultReasoningEffort() != "" || len(efforts) > 0 && !slices.Contains(efforts, model.GetDefaultReasoningEffort()) {
			return invalid()
		}
		result.Models = append(result.Models, platformrepo.ProviderModelCatalogRecord{ID: model.GetId(), ReasoningEfforts: efforts, DefaultReasoningEffort: model.GetDefaultReasoningEffort()})
	}
	return result, nil
}
