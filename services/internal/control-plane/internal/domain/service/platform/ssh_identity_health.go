package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sshIdentityHealthOwner admits only the protected broker's exact method; missing persistence never falls back to TOKEN completion.
// sshIdentityHealthOwner допускает лишь exact method защищённого broker; отсутствие persistence не переключает на TOKEN completion.
func (service *Service) sshIdentityHealthOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.SSHIdentityHealthRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "secret-broker" || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	repository, ok := service.repository.(platformrepo.SSHIdentityHealthRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// BeginSSHIdentityHealthProbe consumes an existing gen1 health binding once, deriving identity and transport mode from owner state.
// BeginSSHIdentityHealthProbe однократно расходует existing gen1 health binding; identity и transport mode выводятся из owner state.
func (service *Service) BeginSSHIdentityHealthProbe(ctx context.Context, p value.Principal, binding managedssh.Binding) (entity.SSHIdentityHealthAdmission, error) {
	repository, p, err := service.sshIdentityHealthOwner(ctx, p, "platform.runtime.ssh.health.begin")
	if err != nil {
		return entity.SSHIdentityHealthAdmission{}, err
	}
	if binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return entity.SSHIdentityHealthAdmission{}, errs.ErrInvalid
	}
	return repository.BeginSSHIdentityHealthProbe(ctx, p, binding)
}

// CompleteSSHIdentityHealthProbe validates the complete outcome union before the owner records a result; unavailable observations never fabricate authentication.
// CompleteSSHIdentityHealthProbe проверяет complete outcome union до записи результата owner; unavailable observation не создаёт authentication.
func (service *Service) CompleteSSHIdentityHealthProbe(ctx context.Context, p value.Principal, input command.SSHIdentityHealthComplete) (entity.SSHIdentityHealthReceipt, error) {
	repository, p, err := service.sshIdentityHealthOwner(ctx, p, "platform.runtime.ssh.health.complete")
	if err != nil {
		return entity.SSHIdentityHealthReceipt{}, err
	}
	if !validSSHHealthCompletion(input) {
		return entity.SSHIdentityHealthReceipt{}, errs.ErrInvalid
	}
	return repository.CompleteSSHIdentityHealthProbe(ctx, p, input)
}

// validSSHHealthCompletion keeps joined authentication evidence separate from ambiguous transport and rejects foreign or partial outcome fields.
// validSSHHealthCompletion отделяет joined authentication evidence от ambiguous transport и отклоняет foreign либо partial outcome fields.
func validSSHHealthCompletion(input command.SSHIdentityHealthComplete) bool {
	if input.Binding.Validate() != nil || input.Binding.ConnectionTestRef == "" || !managedssh.OwnerReference(input.AttemptRef) || !managedssh.Digest(input.SnapshotDigest) {
		return false
	}
	switch input.Outcome {
	case entity.PersonalIntegrationCredentialAuthenticated:
		return managedssh.Digest(input.ObservedSubjectDigest) && !input.QualifiedUntil.IsZero() && input.QualifiedUntil.Nanosecond()%1000 == 0
	case entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable:
		return input.ObservedSubjectDigest == "" && input.QualifiedUntil.IsZero()
	default:
		return false
	}
}
