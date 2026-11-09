package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListIntegrationApprovalConsents resolves the verified principal before reading the specialized self catalog; GET never creates or extends consent.
// ListIntegrationApprovalConsents разрешает verified principal до чтения специализированного self catalog; GET не создаёт и не продлевает consent.
func (service *Service) ListIntegrationApprovalConsents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationApprovalConsent, string, error) {
	reader, ok := service.repository.(port.IntegrationApprovalConsentCatalog)
	if !ok {
		return nil, "", errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return reader.ListIntegrationApprovalConsents(ctx, resolved, filter)
}
