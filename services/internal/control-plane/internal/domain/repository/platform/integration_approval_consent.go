package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// IntegrationApprovalConsentCatalog is the specialized self catalog, separate from broad integration administration or generic resource transitions.
// IntegrationApprovalConsentCatalog — специализированный self catalog, отдельно от broad integration administration и generic resource transitions.
type IntegrationApprovalConsentCatalog interface {
	ListIntegrationApprovalConsents(context.Context, value.Principal, query.Filter) ([]entity.IntegrationApprovalConsent, string, error)
}
