package platform

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sshIdentitySetupOwner resolves the current principal and exact technical permission before the purpose repository port; missing persistence support is unavailable, never generic TOKEN fallback.
// sshIdentitySetupOwner разрешает current principal и exact technical permission до purpose repository port; missing persistence support означает unavailable, а не generic TOKEN fallback.
func (service *Service) sshIdentitySetupOwner(ctx context.Context, p value.Principal, workload, permission string) (platformrepo.SSHIdentitySetupRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != workload || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	repository, ok := service.repository.(platformrepo.SSHIdentitySetupRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// BeginSSHIdentitySetup accepts bounded content preconditions only; actual actor, policy, target and private material identity are resolved atomically before binding OCC/idempotency at the owner.
// BeginSSHIdentitySetup принимает лишь bounded content preconditions; actual actor, policy, target и private material identity разрешаются атомарно до binding OCC/idempotency у owner.
func (service *Service) BeginSSHIdentitySetup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.SSHIdentitySetupBegin) (entity.SSHIdentitySetupAdmission, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.ssh.setup.begin")
	if err != nil {
		return entity.SSHIdentitySetupAdmission{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || input.ExpectedConnectionVersion < 1 || !validSSHSetupMutation(mutation, true) || !managedssh.Digest(input.ContentSHA256) || input.ContentBytes < 1 || input.ContentBytes > 16384 {
		return entity.SSHIdentitySetupAdmission{}, errs.ErrInvalid
	}
	return repository.BeginSSHIdentitySetup(ctx, p, mutation, input)
}

// GetSSHIdentitySetup addresses safe current metadata or an exact Begin receipt; neither route reissues the raw permit or reads another subject's physical material.
// GetSSHIdentitySetup адресует safe current metadata либо exact Begin receipt; оба route не перевыдают raw permit и не читают physical material другого subject.
func (service *Service) GetSSHIdentitySetup(ctx context.Context, p value.Principal, input command.SSHIdentitySetupRead) (entity.SSHIdentitySetupView, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.ssh.setup.get")
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || (input.SetupRef == "") == (input.BeginIdempotencyKey == "") || input.SetupRef != "" && !validSSHSetupRef(input.SetupRef, "ssu_") || input.BeginIdempotencyKey != "" && !validSSHSetupIdempotency(input.BeginIdempotencyKey) {
		return entity.SSHIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.GetSSHIdentitySetup(ctx, p, input)
}

// CancelSSHIdentitySetup closes the current setup without claiming that material creation or remote use was undone; the repository checks addressed ownership before version/idempotency.
// CancelSSHIdentitySetup закрывает current setup без утверждения отмены material creation либо remote use; repository проверяет addressed ownership до version/idempotency.
func (service *Service) CancelSSHIdentitySetup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.SSHIdentitySetupCancel) (entity.SSHIdentitySetupView, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "control-api-gateway", "platform.integrations.ssh.setup.cancel")
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	if !personalIntegrationCredentialCoordinate(input.ConnectionRef) || !validSSHSetupRef(input.SetupRef, "ssu_") || !validSSHSetupMutation(mutation, false) {
		return entity.SSHIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.CancelSSHIdentitySetup(ctx, p, mutation, input)
}

// ResolveSSHIdentityUpload verifies the protected broker route before resolving private setup pins; a permit digest is only a locator and does not itself admit external storage.
// ResolveSSHIdentityUpload проверяет protected broker route до разрешения private setup pins; permit digest является лишь locator и сам не допускает external storage.
func (service *Service) ResolveSSHIdentityUpload(ctx context.Context, p value.Principal, input command.SSHIdentityUploadResolve) (entity.SSHIdentityUploadSnapshot, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.ssh.setup.resolve")
	if err != nil {
		return entity.SSHIdentityUploadSnapshot{}, err
	}
	if !validSSHSetupRef(input.SetupRef, "ssu_") || !managedssh.Digest(input.PermitSHA256) {
		return entity.SSHIdentityUploadSnapshot{}, errs.ErrInvalid
	}
	return repository.ResolveSSHIdentityUpload(ctx, p, input)
}

// ClaimSSHIdentityUpload accepts actual parsed key hashes from the authenticated broker only; no request field chooses mode, actor, physical namespace or a new revision.
// ClaimSSHIdentityUpload принимает actual parsed key hashes лишь от authenticated broker; request field не выбирает mode, actor, physical namespace либо новую revision.
func (service *Service) ClaimSSHIdentityUpload(ctx context.Context, p value.Principal, input command.SSHIdentityUploadClaim) (entity.SSHIdentityUploadAdmission, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.ssh.setup.claim")
	if err != nil {
		return entity.SSHIdentityUploadAdmission{}, err
	}
	if !validSSHSetupRef(input.SetupRef, "ssu_") || !managedssh.Digest(input.PermitSHA256) || !managedssh.Digest(input.SnapshotDigest) || !managedssh.Digest(input.Key.ContentSHA256) || !managedssh.Digest(input.Key.AuthenticationSHA256) || !managedssh.OwnerReference(input.WorkloadInstance) {
		return entity.SSHIdentityUploadAdmission{}, errs.ErrInvalid
	}
	return repository.ClaimSSHIdentityUpload(ctx, p, input)
}

// CompleteSSHIdentityUpload checks the exact outcome union before the owner's fenced transition; late or rejected evidence cannot revive expired grants or silently attach conflicting material.
// CompleteSSHIdentityUpload проверяет exact outcome union до fenced transition owner; late либо rejected evidence не восстанавливает expired grants и не присоединяет conflicting material незаметно.
func (service *Service) CompleteSSHIdentityUpload(ctx context.Context, p value.Principal, input command.SSHIdentityUploadComplete) (entity.SSHIdentitySetupView, error) {
	repository, p, err := service.sshIdentitySetupOwner(ctx, p, "secret-broker", "platform.runtime.ssh.setup.complete")
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	if !validSSHSetupRef(input.SetupRef, "ssu_") || !validSSHSetupRef(input.AttemptRef, "ssa_") || input.Generation < 1 || input.Generation > 8 || !managedssh.OwnerReference(input.Fence) || !managedssh.OwnerReference(input.WorkloadInstance) || !validSSHIdentityCompletion(input) {
		return entity.SSHIdentitySetupView{}, errs.ErrInvalid
	}
	return repository.CompleteSSHIdentityUpload(ctx, p, input)
}

// validSSHIdentityCompletion enforces a complete closed receipt oneof without inventing locator fields for unknown or rejected acknowledgements.
// validSSHIdentityCompletion обеспечивает complete closed receipt oneof без изготовления locator fields для unknown либо rejected acknowledgements.
func validSSHIdentityCompletion(input command.SSHIdentityUploadComplete) bool {
	outcomes := 0
	if input.Identity != nil {
		if !input.Identity.Valid() {
			return false
		}
		outcomes++
	}
	if input.Unknown {
		outcomes++
	}
	if input.Rejected != nil {
		if !managedssh.Digest(input.Rejected.ResponseSHA256) || len(input.Rejected.ObservedUID) > 256 || !utf8.ValidString(input.Rejected.ObservedUID) {
			return false
		}
		outcomes++
	}
	return outcomes == 1
}

// validSSHSetupRef accepts only the purpose-specific owner-assigned URL segment, not an authority-bearing actor or root identifier.
// validSSHSetupRef допускает лишь purpose-specific owner-assigned URL segment, а не authority-bearing actor либо root identifier.
func validSSHSetupRef(ref, prefix string) bool {
	return len(ref) >= len(prefix)+8 && strings.HasPrefix(ref, prefix) && personalIntegrationCredentialCoordinate(ref)
}

// validSSHSetupMutation distinguishes explicitly absent-binding OCC from an omitted version; cancel always requires a positive setup version.
// validSSHSetupMutation отличает explicitly absent-binding OCC от omitted version; cancel всегда требует positive setup version.
func validSSHSetupMutation(mutation value.Mutation, allowZero bool) bool {
	return mutation.ExpectedVersion != nil && *mutation.ExpectedVersion >= 0 && (allowZero || *mutation.ExpectedVersion > 0) && validSSHSetupIdempotency(mutation.IdempotencyKey)
}

// validSSHSetupIdempotency bounds exact receipt coordinates without trimming, folding or accepting control characters.
// validSSHSetupIdempotency ограничивает exact receipt coordinates без trimming, folding либо control characters.
func validSSHSetupIdempotency(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && utf8.ValidString(key) && strings.IndexFunc(key, unicode.IsControl) < 0
}
