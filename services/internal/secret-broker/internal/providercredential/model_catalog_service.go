package providercredential

import (
	"context"
	"errors"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

func (service *Service) ObserveModelCatalog(ctx context.Context, accountRef string, descriptor kubernetesstore.ProviderCredentialDescriptor, method string) (ModelCatalog, error) {
	if ctx == nil || service == nil || service.lifecycle.Err() != nil || ctx.Err() != nil {
		return ModelCatalog{}, errors.New("provider model catalog lifecycle is unavailable")
	}
	if method != CatalogMethodAPIKey && method != CatalogMethodDeviceCode {
		return ModelCatalog{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, modelCatalogTimeout)
	defer cancel()
	stop := context.AfterFunc(service.lifecycle, cancel)
	defer stop()
	started := time.Now().UTC()
	raw, err := service.store.ReadProviderCredentialExact(ctx, accountRef, descriptor)
	if err != nil {
		return catalogFailure(ctx, err)
	}
	defer clear(raw)
	result, err := service.appServer.ObserveModelCatalog(ctx, raw, method)
	if err != nil {
		return catalogFailure(ctx, err)
	}
	if ctx.Err() != nil {
		return ModelCatalog{}, ctx.Err()
	}
	if result.ObservedAt.IsZero() || result.ObservedAt.Before(started) || result.ObservedAt.After(time.Now().UTC()) {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	if result.Failure == CatalogFailureNone {
		if result.Source != CatalogRemoteAPI && result.Source != CatalogRemoteCodex || validateCatalogModels(result.Models) != nil {
			return catalogFailure(ctx, errModelCatalogUnverified)
		}
		if method == CatalogMethodAPIKey && result.Source != CatalogRemoteAPI || method == CatalogMethodDeviceCode && result.Source != CatalogRemoteCodex {
			return catalogFailure(ctx, errModelCatalogUnverified)
		}
	} else {
		if result.Source != "" || len(result.Models) != 0 {
			return catalogFailure(ctx, errModelCatalogUnverified)
		}
		switch result.Failure {
		case CatalogFailureUnavailable, CatalogFailureUnverified, CatalogFailureAuthorization:
		default:
			return catalogFailure(ctx, errModelCatalogUnverified)
		}
	}
	return result, nil
}
