package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListArtifactBindingTargets resolves principal and reads the owner-computed eligible binding
// target projection with the supplied artifact and filters.
// ListArtifactBindingTargets разрешает principal и читает вычисленную владельцем проекцию
// доступных целей привязки с указанными артефактом и фильтрами.
func (service *Service) ListArtifactBindingTargets(ctx context.Context, p value.Principal, artifactRef string, filter query.Filter) (entity.ArtifactBindingTargets, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ArtifactBindingTargets{}, err
	}
	return service.repository.ListArtifactBindingTargets(ctx, p, artifactRef, filter)
}

// GetRunAttachmentEligibility resolves principal and delegates project/target/run attachment
// eligibility to the owner without deriving rights from supplied locators.
// GetRunAttachmentEligibility разрешает principal и передаёт доступность вложений проекта, цели и
// выполнения владельцу без вывода прав из переданных locators.
func (service *Service) GetRunAttachmentEligibility(ctx context.Context, p value.Principal, projectRef string, target entity.RunTarget, runRef string) (entity.RunAttachmentEligibility, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RunAttachmentEligibility{}, err
	}
	return service.repository.GetRunAttachmentEligibility(ctx, p, projectRef, target, runRef)
}
