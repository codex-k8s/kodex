package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) ListIntegrationGrantConnectionCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationConnectionCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationConnectionCandidates{}, err
	}
	return service.repository.ListIntegrationGrantConnectionCandidates(ctx, p, input)
}

func (service *Service) ListIntegrationGrantProjectCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationProjectCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationProjectCandidates{}, err
	}
	return service.repository.ListIntegrationGrantProjectCandidates(ctx, p, input)
}

func (service *Service) ListIntegrationGrantRecipientCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationRecipientCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationRecipientCandidates{}, err
	}
	return service.repository.ListIntegrationGrantRecipientCandidates(ctx, p, input)
}

func (service *Service) ListIntegrationGrantCapabilityCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationCapabilityCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationCapabilityCandidates{}, err
	}
	return service.repository.ListIntegrationGrantCapabilityCandidates(ctx, p, input)
}
