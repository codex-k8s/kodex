package providercredentialclient

import (
	"context"
	"errors"
	"slices"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// invalidModelCatalog fixes the bounded diagnostic for invalid model-catalog bindings or observations.
// invalidModelCatalog задаёт ограниченную диагностику неверных привязок или наблюдений каталога моделей.
const invalidModelCatalog = "provider model catalog binding is invalid"

// modelCatalogRequest validates the authorization method, exact task/account/credential/claim pins, bounded versions and timestamp representation.
// It constructs the provider observation request without probing expiry freshness or authorizing access itself.
// modelCatalogRequest проверяет способ авторизации, точные привязки задачи/аккаунта/учётных данных/заявки, ограниченные версии и запись времени.
// Создаётся запрос наблюдения провайдера без проверки свежести срока или самостоятельного разрешения доступа.
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

// ModelCatalogRequestDigest validates and deterministically hashes the exact model-catalog observation request.
// The digest is request binding, not a catalog content digest or a proof of a verified provider source.
// ModelCatalogRequestDigest проверяет и детерминированно хеширует точный запрос наблюдения каталога моделей.
// Дайджест привязывает запрос, а не содержимое каталога и не доказывает проверенный источник провайдера.
func (client *Client) ModelCatalogRequestDigest(task platformrepo.ProviderModelCatalogTask) (string, error) {
	request, err := modelCatalogRequest(task)
	if err != nil {
		return "", err
	}
	return deterministicRequestDigest(request)
}

// ObserveProviderModelCatalog requires the recomputed request digest to match the task before one broker RPC.
// It validates the returned observation without retrying, inventing models or changing the owner task.
// ObserveProviderModelCatalog требует совпадения заново вычисленного дайджеста с задачей до одного RPC брокера.
// Возвращённое наблюдение проверяется без повтора, выдумывания моделей или изменения задачи владельца.
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

// modelCatalogObservation checks account/credential binding, timestamp syntax, count limits and the allowed source/failure combination.
// It rejects duplicate model/effort IDs and multiple defaults, sorts copied efforts and preserves capability/default metadata without claiming response freshness or signature verification.
// modelCatalogObservation проверяет привязки аккаунта/учётных данных, синтаксис времени, лимиты количества и допустимое сочетание источника/ошибки.
// Отклоняются повторные модели/степени и несколько моделей по умолчанию, копии степеней сортируются и метаданные сохраняются без заявления свежести ответа или проверки подписи.
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
	defaults := 0
	for _, model := range response.GetModels() {
		if model == nil || !validBoundedSafeText(model.GetId(), 200) || seen[model.GetId()] || len(model.GetReasoningEfforts()) > 16 {
			return invalid()
		}
		seen[model.GetId()] = true
		if model.GetIsDefault() {
			defaults++
			if defaults > 1 {
				return invalid()
			}
		}
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
		result.Models = append(result.Models, platformrepo.ProviderModelCatalogRecord{ID: model.GetId(), ReasoningEfforts: efforts, DefaultReasoningEffort: model.GetDefaultReasoningEffort(), IsDefault: model.GetIsDefault()})
	}
	return result, nil
}
