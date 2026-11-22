package sshidentity

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
)

// ExecutionService owns one synchronous finite operation and its joined cleanup; the owner alone selects identity, target, recipe, Gate and publication authority.
// ExecutionService владеет одной synchronous finite operation и её joined cleanup; лишь owner выбирает identity, target, recipe, Gate и publication authority.
type ExecutionService struct {
	owner  sshrepository.IdentityExecutionOwner
	reader sshrepository.IdentityMaterialReader
	keys   sshrepository.IdentityKeyPreparer
	native sshrepository.IdentityNativeExecutor
}

// NewExecutionService requires every purpose-specific protected port without starting a worker, touching a key or dialing a target.
// NewExecutionService требует все purpose-specific protected ports без запуска worker, чтения key либо подключения к target.
func NewExecutionService(owner sshrepository.IdentityExecutionOwner, reader sshrepository.IdentityMaterialReader, keys sshrepository.IdentityKeyPreparer, native sshrepository.IdentityNativeExecutor) (*ExecutionService, error) {
	if owner == nil || reader == nil || keys == nil || native == nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	return &ExecutionService{owner: owner, reader: reader, keys: keys, native: native}, nil
}

// Execute begins once before material access, performs one native dispatch with fresh original owner checks and records completion after the local transport joins; ambiguous Begin never retries.
// Execute выполняет Begin один раз до material access, один native dispatch со fresh original owner checks и сохраняет completion после local transport join; ambiguous Begin не повторяется.
func (service *ExecutionService) Execute(ctx context.Context, binding managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	if ctx == nil || ctx.Err() != nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	admission, err := service.owner.BeginSSHExecution(ctx, binding)
	if err != nil {
		return managedssh.ExecutionReceipt{}, err
	}
	defer clear(admission.CanonicalInput)
	if admission.ReceiptSource != nil {
		defer clear(admission.ReceiptSource.CanonicalInput)
	}
	if admission.Validate(time.Now()) != nil || admission.InvocationRef != binding.InvocationRef || admission.InputDigest != binding.InputDigest || admission.Recover {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	defer cancel()
	observation := managedssh.ExecutionObservation{Outcome: managedssh.ExecutionFailed}
	material, readErr := service.reader.ReadSSHIdentity(operation, admission.Identity)
	if readErr == nil {
		key, keyErr := service.keys.PrepareSSHIdentity(material)
		if len(material) > 0 && len(material) <= 16384 && keyErr == nil && key.ContentSHA256 == admission.Identity.ContentSHA256 && key.AuthenticationSHA256 == admission.Identity.AuthenticationSHA256 {
			observation, err = service.native.ExecuteSSHIdentity(operation, admission, material, func(checkContext context.Context) error {
				receipt, err := service.owner.GetSSHExecution(checkContext, binding)
				if err != nil {
					return err
				}
				if receipt.Validate(time.Now(), false) != nil || receipt.AttemptRef != admission.AttemptRef || receipt.SnapshotDigest != admission.SnapshotDigest || receipt.Recorded || receipt.State != "UNREPORTED" || !receipt.AuthorizedUntil.Equal(admission.ExpiresAt) {
					return sshrepository.ErrIdentityRejected
				}
				return nil
			})
			if err != nil {
				clear(material)
				// Owner-read infrastructure failure leaves the intent UNREPORTED instead of inventing a terminal authority transition.
				// Infrastructure failure owner-read оставляет intent UNREPORTED вместо придуманного terminal authority transition.
				if errors.Is(err, sshrepository.ErrIdentityUnavailable) {
					return managedssh.ExecutionReceipt{}, err
				}
				observation = managedssh.ExecutionObservation{Outcome: managedssh.ExecutionFailed}
			}
		}
	}
	clear(material)
	if observation.Validate() != nil {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	defer clear(observation.Result)
	cancel()
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer done()
	complete, stopComplete := context.WithTimeout(cleanup, 3*time.Second)
	result, err := service.owner.CompleteSSHExecution(complete, binding, admission, observation)
	stopComplete()
	if err != nil {
		if !errors.Is(err, sshrepository.ErrIdentityUnavailable) {
			return managedssh.ExecutionReceipt{}, err
		}
		// A single metadata read resolves only an exact committed observation; no effect or completion is resent.
		// Одно metadata read разрешает лишь exact committed observation; effect и completion не отправляются повторно.
		result, err = service.owner.GetSSHExecution(cleanup, binding)
		if err != nil {
			return managedssh.ExecutionReceipt{}, err
		}
	}
	if result.Validate(time.Now(), true) != nil || !result.Recorded || result.AttemptRef != admission.AttemptRef || result.SnapshotDigest != admission.SnapshotDigest || result.Observation.Outcome != observation.Outcome || result.Observation.ResultDigest != observation.ResultDigest {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	return result, nil
}

// Get reads exact original receipt metadata without repeating admission, material access, completion or the external operation; active source credit remains private to the broker.
// Get читает exact original receipt metadata без повторных admission, material access, completion либо external operation; active source credit остаётся private broker.
func (service *ExecutionService) Get(ctx context.Context, binding managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	if ctx == nil || ctx.Err() != nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	receipt, err := service.owner.GetSSHExecution(ctx, binding)
	if err != nil {
		return managedssh.ExecutionReceipt{}, err
	}
	if receipt.Validate(time.Now(), false) != nil {
		return managedssh.ExecutionReceipt{}, sshrepository.ErrIdentityRejected
	}
	receipt.AuthorizedUntil = time.Time{}
	return receipt, nil
}
