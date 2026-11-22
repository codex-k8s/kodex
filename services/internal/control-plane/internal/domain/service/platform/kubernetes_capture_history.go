package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListKubernetesCaptures normalizes the verified caller and refuses absent owner composition or invalid paging.
// History discovery grants neither integration execution nor artifact bytes; the owner rechecks each row.
// ListKubernetesCaptures нормализует verified caller и отклоняет absent owner composition либо invalid paging.
// History discovery не выдаёт integration execution либо artifact bytes; owner перепроверяет каждую строку.
func (service *Service) ListKubernetesCaptures(ctx context.Context, p value.Principal, input query.KubernetesCaptureList) (entity.KubernetesCapturePage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.KubernetesCapturePage{}, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.integrations.kubernetes.capture.list" {
		return entity.KubernetesCapturePage{}, errs.ErrForbidden
	}
	if !validKubernetesSetupRef(input.RunRef, "run_") || !validKubernetesSetupRef(input.NodeRef, "nod_") || input.Page.Size < 0 || input.Page.Size > 50 || input.Page.Token != "" && !validKubernetesSetupRef(input.Page.Token, "koc_") {
		return entity.KubernetesCapturePage{}, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.KubernetesCaptureHistoryRepository)
	if !ok {
		return entity.KubernetesCapturePage{}, errs.ErrUnavailable
	}
	return repository.ListKubernetesCaptures(ctx, p, input)
}
