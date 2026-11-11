package providercredential

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// ObserveModelCatalogForDefinition ties observation to service/caller lifetime, reads the exact owner-selected
// credential once and clears that copy, then validates observation time and
// method/source/failure/capability consistency. It does not grant account access or refresh tokens;
// invalid or partial results become safe metadata-only failures.
// ObserveModelCatalogForDefinition связывает наблюдение со service/caller lifetime, один раз читает точный выбранный
// владельцем credential и обнуляет копию, затем проверяет время и согласованность
// method/source/failure/capabilities. Доступ account и token refresh не выдаются; неверный или частичный
// результат становится безопасным отказом только с metadata.
// ObserveModelCatalogForDefinition verifies owner-selected engine affinity before reading or dispatching secret material.
// ObserveModelCatalogForDefinition проверяет owner-selected engine affinity до чтения и dispatch secret material.
func (service *Service) ObserveModelCatalogForDefinition(ctx context.Context, accountRef string, descriptor kubernetesstore.ProviderCredentialDescriptor, method, definitionKey string) (ModelCatalog, error) {
	if ctx == nil || service == nil || service.lifecycle.Err() != nil || ctx.Err() != nil {
		return ModelCatalog{}, errors.New("provider model catalog lifecycle is unavailable")
	}
	if method != CatalogMethodAPIKey || definitionKey == "" || service.modelProvider == nil || definitionKey != service.modelProvider.DefinitionKey() {
		return ModelCatalog{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, modelCatalogTimeout)
	defer cancel()
	stop := context.AfterFunc(service.lifecycle, cancel)
	defer stop()
	started := time.Now().UTC()
	raw, err := service.store.ReadProviderCredentialExact(ctx, accountRef, descriptor)
	if err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageCredential, err))
	}
	defer clear(raw)
	key, decodeErr := runtimecontract.DecodeGatewayProviderAuthentication(raw, definitionKey)
	clear(key)
	if decodeErr != nil {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	result, err := service.observeGatewayCatalog(ctx, raw)
	if err != nil {
		return catalogFailure(ctx, err)
	}
	if ctx.Err() != nil {
		return ModelCatalog{}, ctx.Err()
	}
	if result.ObservedAt.IsZero() || result.ObservedAt.Before(started) || result.ObservedAt.After(time.Now().UTC()) {
		return catalogFailure(ctx, atCatalogStage(catalogStageResult, errModelCatalogUnverified))
	}
	if result.Failure == CatalogFailureNone {
		if result.Source != CatalogRemoteAPI || validateCatalogModels(result.Models) != nil {
			return catalogFailure(ctx, atCatalogStage(catalogStageResult, errModelCatalogUnverified))
		}
	} else {
		if result.Source != "" || len(result.Models) != 0 {
			return catalogFailure(ctx, atCatalogStage(catalogStageResult, errModelCatalogUnverified))
		}
		switch result.Failure {
		case CatalogFailureUnavailable, CatalogFailureUnverified, CatalogFailureAuthorization:
		default:
			return catalogFailure(ctx, atCatalogStage(catalogStageResult, errModelCatalogUnverified))
		}
	}
	return result, nil
}
