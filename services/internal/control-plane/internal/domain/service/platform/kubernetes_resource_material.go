package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesResourceMaterialOwner requires the exact authenticated gateway operation before
// touching the optional owner repository. Missing persistence fails closed, never as READY.
// kubernetesResourceMaterialOwner требует exact authenticated gateway operation до обращения
// к optional owner repository. Missing persistence закрыто отклоняется, не выдаётся за READY.
func (service *Service) kubernetesResourceMaterialOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.KubernetesResourceMaterialRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	repository, available := service.repository.(platformrepo.KubernetesResourceMaterialRepository)
	if !available {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// BeginKubernetesResourceMaterial forwards only an exact bounded private-upload proposal.
// The repository derives actor/policy/identity/target afresh; this command cannot create a Secret.
// BeginKubernetesResourceMaterial передаёт только exact bounded private-upload proposal.
// Repository свежо выводит actor/policy/identity/target; command не создаёт target Secret.
func (service *Service) BeginKubernetesResourceMaterial(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesResourceMaterialBegin) (entity.KubernetesResourceMaterialAdmission, error) {
	repository, p, err := service.kubernetesResourceMaterialOwner(ctx, p, "platform.integrations.kubernetes.material.begin")
	if err != nil {
		return entity.KubernetesResourceMaterialAdmission{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || input.ExpectedConnectionVersion < 1 || !validKubernetesSetupMutation(mutation, true) || !input.ValidDestination() || !managedkubernetes.ValidDigest(input.ContentSHA256) || input.ContentBytes == 0 || input.ContentBytes > 1<<20 {
		return entity.KubernetesResourceMaterialAdmission{}, errs.ErrInvalid
	}
	return repository.BeginKubernetesResourceMaterial(ctx, p, mutation, input)
}

// GetKubernetesResourceMaterial reads one current owner-local locator or original Begin key.
// It returns metadata only and cannot recover private bytes or another upload permit.
// GetKubernetesResourceMaterial читает один current owner-local locator либо original Begin key.
// Возвращает metadata и не восстанавливает private bytes либо другой upload permit.
func (service *Service) GetKubernetesResourceMaterial(ctx context.Context, p value.Principal, input command.KubernetesResourceMaterialRead) (entity.KubernetesResourceMaterialView, error) {
	repository, p, err := service.kubernetesResourceMaterialOwner(ctx, p, "platform.integrations.kubernetes.material.get")
	if err != nil {
		return entity.KubernetesResourceMaterialView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || (input.MaterialRef == "") == (input.BeginIdempotencyKey == "") || input.MaterialRef != "" && !validKubernetesSetupRef(input.MaterialRef, "krs_") || input.BeginIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.BeginIdempotencyKey) {
		return entity.KubernetesResourceMaterialView{}, errs.ErrInvalid
	}
	return repository.GetKubernetesResourceMaterial(ctx, p, input)
}

// CancelKubernetesResourceMaterial preserves exact artifact addressing and OCC/idempotency.
// Prior UNKNOWN/consumed effect facts remain immutable; cancellation cannot undo a remote write.
// CancelKubernetesResourceMaterial сохраняет exact artifact addressing и OCC/idempotency.
// Prior UNKNOWN/consumed effect facts immutable; cancellation не отменяет remote write.
func (service *Service) CancelKubernetesResourceMaterial(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesResourceMaterialCancel) (entity.KubernetesResourceMaterialView, error) {
	repository, p, err := service.kubernetesResourceMaterialOwner(ctx, p, "platform.integrations.kubernetes.material.cancel")
	if err != nil {
		return entity.KubernetesResourceMaterialView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || !validKubernetesSetupRef(input.MaterialRef, "krs_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesResourceMaterialView{}, errs.ErrInvalid
	}
	return repository.CancelKubernetesResourceMaterial(ctx, p, mutation, input)
}
