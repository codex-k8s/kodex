package platform

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesIdentitySetupOwner checks exact technical caller/permission before repository access.
// The repository must resolve the actual user/service and current Connection inside its transaction.
// kubernetesIdentitySetupOwner проверяет exact technical caller/permission до repository access.
// Repository разрешает actual user/service и current Connection внутри своей transaction.
func (service *Service) kubernetesIdentitySetupOwner(ctx context.Context, principal value.Principal, workload, permission string) (platformrepo.KubernetesIdentitySetupRepository, value.Principal, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, principal, err
	}
	if principal.CallerWorkload != workload || principal.Permission != permission {
		return nil, principal, errs.ErrForbidden
	}
	repository, exists := service.repository.(platformrepo.KubernetesIdentitySetupRepository)
	if !exists {
		return nil, principal, errs.ErrUnavailable
	}
	return repository, principal, nil
}

// BeginKubernetesIdentitySetup admits only metadata preconditions from the authenticated owner path.
// No target, other subject, private namespace, credential kind or material comes from its payload.
// BeginKubernetesIdentitySetup допускает только metadata preconditions authenticated owner path.
// Target, чужой subject, private namespace, credential kind и material отсутствуют в payload.
func (service *Service) BeginKubernetesIdentitySetup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesIdentitySetupBegin) (entity.KubernetesIdentitySetupAdmission, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.kubernetes.setup.begin")
	if err != nil {
		return entity.KubernetesIdentitySetupAdmission{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || input.ExpectedConnectionVersion < 1 ||
		!validKubernetesSetupMutation(mutation, true) || !managedkubernetes.ValidDigest(input.ContentSHA256) ||
		input.ContentBytes == 0 || input.ContentBytes > managedkubernetes.MaximumCredentialBytes {
		return entity.KubernetesIdentitySetupAdmission{}, errs.ErrInvalid
	}
	return repository.BeginKubernetesIdentitySetup(ctx, p, mutation, input)
}

// GetKubernetesIdentitySetup accepts one self-scoped locator or the original Begin receipt key.
// Neither selector can recover an upload permit or material; visibility remains a fresh owner read.
// GetKubernetesIdentitySetup принимает один self-scoped locator либо исходный Begin receipt key.
// Selector не восстанавливает upload permit либо material; visibility остаётся fresh owner read.
func (service *Service) GetKubernetesIdentitySetup(ctx context.Context, p value.Principal, input command.KubernetesIdentitySetupRead) (entity.KubernetesIdentitySetupView, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.kubernetes.setup.get")
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || (input.SetupRef == "") == (input.BeginIdempotencyKey == "") ||
		(input.SetupRef != "" && !validKubernetesSetupRef(input.SetupRef, "ksu_")) ||
		(input.BeginIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.BeginIdempotencyKey)) {
		return entity.KubernetesIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.GetKubernetesIdentitySetup(ctx, p, input)
}

// CancelKubernetesIdentitySetup checks setup syntax without mistaking OCC for ownership.
// Its transaction closes all attempts, while late material evidence cannot revive the setup.
// CancelKubernetesIdentitySetup проверяет setup syntax, не принимая OCC за ownership.
// Transaction закрывает все attempts; late material evidence не восстанавливает setup.
func (service *Service) CancelKubernetesIdentitySetup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesIdentitySetupCancel) (entity.KubernetesIdentitySetupView, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.kubernetes.setup.cancel")
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || !validKubernetesSetupRef(input.SetupRef, "ksu_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.CancelKubernetesIdentitySetup(ctx, p, mutation, input)
}

// ResolveKubernetesIdentityUpload reads broker-private setup pins after current permit validation.
// A syntactically valid permit digest is a locator and cannot itself grant an external write.
// ResolveKubernetesIdentityUpload читает broker-private setup pins после current permit validation.
// Корректная форма permit digest — locator, а не разрешение на external write.
func (service *Service) ResolveKubernetesIdentityUpload(ctx context.Context, p value.Principal, input command.KubernetesIdentityUploadResolve) (entity.KubernetesIdentityUploadSnapshot, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.kubernetes.setup.resolve")
	if err != nil {
		return entity.KubernetesIdentityUploadSnapshot{}, err
	}
	if !validKubernetesSetupRef(input.SetupRef, "ksu_") || !managedkubernetes.ValidDigest(input.PermitSHA256) {
		return entity.KubernetesIdentityUploadSnapshot{}, errs.ErrInvalid
	}
	return repository.ResolveKubernetesIdentityUpload(ctx, p, input)
}

// ClaimKubernetesIdentityUpload bounds one immutable request before the durable WRITE/OBSERVE choice.
// Neither a workload locator nor a repeated claim can authorize a second material create.
// ClaimKubernetesIdentityUpload ограничивает immutable request до durable WRITE/OBSERVE решения.
// Workload locator и повторный claim не разрешают второй material create.
func (service *Service) ClaimKubernetesIdentityUpload(ctx context.Context, p value.Principal, input command.KubernetesIdentityUploadClaim) (entity.KubernetesIdentityUploadAdmission, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.kubernetes.setup.claim")
	if err != nil {
		return entity.KubernetesIdentityUploadAdmission{}, err
	}
	if !validKubernetesSetupRef(input.SetupRef, "ksu_") || !managedkubernetes.ValidDigest(input.PermitSHA256) ||
		!managedkubernetes.ValidDigest(input.SnapshotDigest) || !managedkubernetes.ValidDigest(input.ContentSHA256) || !managedkubernetes.ValidLocator(input.WorkloadInstance) {
		return entity.KubernetesIdentityUploadAdmission{}, errs.ErrInvalid
	}
	return repository.ClaimKubernetesIdentityUpload(ctx, p, input)
}

// CompleteKubernetesIdentityUpload reports exact private storage metadata or UNKNOWN for an admitted attempt.
// Current owner, purpose, expiry and generation checks happen again before atomic binding commit.
// CompleteKubernetesIdentityUpload передаёт exact private storage metadata либо UNKNOWN admitted attempt.
// Current owner, purpose, expiry и generation снова проверяются до atomic binding commit.
func (service *Service) CompleteKubernetesIdentityUpload(ctx context.Context, p value.Principal, input command.KubernetesIdentityUploadComplete) (entity.KubernetesIdentitySetupView, error) {
	repository, p, err := service.kubernetesIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.kubernetes.setup.complete")
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	if !validKubernetesSetupRef(input.SetupRef, "ksu_") || !validKubernetesSetupRef(input.AttemptRef, "ksa_") || input.Generation < 1 || input.Generation > 8 ||
		!managedkubernetes.ValidLocator(input.Fence) || !managedkubernetes.ValidLocator(input.WorkloadInstance) ||
		!input.ValidOutcome() {
		return entity.KubernetesIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.CompleteKubernetesIdentityUpload(ctx, p, input)
}

// validKubernetesSetupRef admits only one bounded owner-assigned setup/attempt URL segment.
// validKubernetesSetupRef допускает только bounded owner-assigned setup/attempt URL segment.
func validKubernetesSetupRef(ref, prefix string) bool {
	return len(ref) >= len(prefix)+8 && strings.HasPrefix(ref, prefix) && personalIntegrationCredentialCoordinate(ref)
}

// validKubernetesSetupMutation keeps explicit absent-binding OCC separate from a missing version.
// validKubernetesSetupMutation отличает явный absent-binding OCC от отсутствующей version.
func validKubernetesSetupMutation(mutation value.Mutation, allowZero bool) bool {
	return mutation.ExpectedVersion != nil && *mutation.ExpectedVersion >= 0 &&
		(allowZero || *mutation.ExpectedVersion > 0) && validKubernetesSetupIdempotency(mutation.IdempotencyKey)
}

// validKubernetesSetupIdempotency rejects malformed bounded receipt coordinates without normalization.
// validKubernetesSetupIdempotency отклоняет malformed bounded receipt coordinates без normalization.
func validKubernetesSetupIdempotency(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && utf8.ValidString(key) && !strings.ContainsAny(key, "\x00\n\r")
}
