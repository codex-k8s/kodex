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

// sshExecutionOwner admits only the exact protected broker operation and organization scope; an absent owner never falls back to generic TOKEN completion.
// sshExecutionOwner допускает лишь exact protected broker operation и organization scope; отсутствие owner не переключает на generic TOKEN completion.
func (service *Service) sshExecutionOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.SSHExecutionRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "secret-broker" || p.Permission != permission || p.ProjectRef != "" {
		return nil, p, errs.ErrForbidden
	}
	repository, ok := service.repository.(platformrepo.SSHExecutionRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// BeginSSHExecution accepts one original invocation lease and releases private coordinates only after the durable owner's sole-dispatch commit.
// BeginSSHExecution принимает original invocation lease и выдаёт private coordinates лишь после durable owner sole-dispatch commit.
func (service *Service) BeginSSHExecution(ctx context.Context, p value.Principal, binding managedssh.Binding) (managedssh.ExecutionAdmission, error) {
	repository, p, err := service.sshExecutionOwner(ctx, p, "platform.runtime.ssh.execution.begin")
	if err != nil {
		return managedssh.ExecutionAdmission{}, err
	}
	if binding.Validate() != nil || binding.InvocationRef == "" {
		return managedssh.ExecutionAdmission{}, errs.ErrInvalid
	}
	return repository.BeginSSHExecution(ctx, p, binding)
}

// CompleteSSHExecution rejects mixed or altered observations before asking the owner to commit a result; a valid envelope itself grants no publication authority.
// CompleteSSHExecution отвергает mixed либо altered observations до owner result commit; valid envelope сам не выдаёт publication authority.
func (service *Service) CompleteSSHExecution(ctx context.Context, p value.Principal, input command.SSHExecutionComplete) (entity.SSHExecutionReceipt, error) {
	repository, p, err := service.sshExecutionOwner(ctx, p, "platform.runtime.ssh.execution.complete")
	if err != nil {
		return entity.SSHExecutionReceipt{}, err
	}
	if input.Binding.Validate() != nil || input.Binding.InvocationRef == "" || !managedssh.OwnerReference(input.AttemptRef) || !managedssh.Digest(input.SnapshotDigest) || input.Observation.Validate() != nil {
		return entity.SSHExecutionReceipt{}, errs.ErrInvalid
	}
	return repository.CompleteSSHExecution(ctx, p, input)
}

// GetSSHExecution reads the original receipt metadata without renewing its lease, releasing output, or granting a lost-acknowledgement dispatch retry.
// GetSSHExecution читает original receipt metadata без renewal lease, выдачи output либо dispatch retry после lost acknowledgement.
func (service *Service) GetSSHExecution(ctx context.Context, p value.Principal, binding managedssh.Binding) (entity.SSHExecutionReceipt, error) {
	repository, p, err := service.sshExecutionOwner(ctx, p, "platform.runtime.ssh.execution.get")
	if err != nil {
		return entity.SSHExecutionReceipt{}, err
	}
	if binding.Validate() != nil || binding.InvocationRef == "" {
		return entity.SSHExecutionReceipt{}, errs.ErrInvalid
	}
	return repository.GetSSHExecution(ctx, p, binding)
}
