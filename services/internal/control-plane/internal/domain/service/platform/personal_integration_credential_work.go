package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimPersonalIntegrationCredentialTests resolves the worker principal before delegating bounded
// owner claims. The dedicated port never accepts a browser-selected user or credential descriptor.
// ClaimPersonalIntegrationCredentialTests разрешает worker principal до bounded owner claims.
// Выделенный порт не принимает выбранного browser пользователя либо credential descriptor.
func (service *Service) ClaimPersonalIntegrationCredentialTests(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	work, ok := service.repository.(port.PersonalIntegrationCredentialWork)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return work.ClaimPersonalIntegrationCredentialTests(ctx, resolved, instance, limit)
}

// BeginPersonalIntegrationCredentialProbe resolves the exact worker identity before a one-time
// owner admission, separate from claiming and completing the health task.
// BeginPersonalIntegrationCredentialProbe разрешает exact worker identity перед одноразовым
// owner admission, отдельно от получения и завершения health task.
func (service *Service) BeginPersonalIntegrationCredentialProbe(ctx context.Context, p value.Principal, input command.PersonalIntegrationCredentialTestCompletion) error {
	work, ok := service.repository.(port.PersonalIntegrationCredentialWork)
	if !ok {
		return errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, p)
	if err != nil {
		return err
	}
	return work.BeginPersonalIntegrationCredentialProbe(ctx, resolved, input)
}
