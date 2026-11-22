package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
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
// owner admission for trusted in-process callers, separate from claiming and completing the health task; gRPC requires its explicit capability through AdmitPersonalIntegrationCredentialProbe.
// BeginPersonalIntegrationCredentialProbe разрешает exact worker identity перед одноразовым
// owner admission для trusted in-process callers, отдельно от получения и завершения health task; gRPC передаёт явную capability через AdmitPersonalIntegrationCredentialProbe.
func (service *Service) BeginPersonalIntegrationCredentialProbe(ctx context.Context, p value.Principal, input command.PersonalIntegrationCredentialTestCompletion) error {
	if input.Outcome != "" {
		return errs.ErrInvalid
	}
	_, err := service.AdmitPersonalIntegrationCredentialProbe(ctx, p, command.PersonalIntegrationCredentialProbeAdmission{
		TestRef: input.TestRef, LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation,
		ContractVersion: command.PersonalIntegrationCredentialProbeContractVersion,
	})
	return err
}

// AdmitPersonalIntegrationCredentialProbe resolves a version-capable trusted worker and returns its fresh finite allowance; transport callers must supply their explicit wire capability.
// AdmitPersonalIntegrationCredentialProbe разрешает trusted worker с поддерживаемой версией и возвращает свежий finite allowance; transport caller передаёт явную wire capability.
func (service *Service) AdmitPersonalIntegrationCredentialProbe(ctx context.Context, p value.Principal, input command.PersonalIntegrationCredentialProbeAdmission) (entity.PersonalIntegrationCredentialProbeAdmissionResult, error) {
	var empty entity.PersonalIntegrationCredentialProbeAdmissionResult
	if input.ContractVersion != command.PersonalIntegrationCredentialProbeContractVersion {
		return empty, errs.ErrForbidden
	}
	work, ok := service.repository.(port.PersonalIntegrationCredentialWork)
	if !ok {
		return empty, errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, p)
	if err != nil {
		return empty, err
	}
	return work.BeginPersonalIntegrationCredentialProbe(ctx, resolved, input)
}
