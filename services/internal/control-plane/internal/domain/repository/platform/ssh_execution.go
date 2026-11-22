package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHExecutionRepository owns the finite invocation admission and completion ledger; private material is addressed only through the immutable purpose-specific pin.
// SSHExecutionRepository владеет finite invocation admission и completion ledger; private material адресуется только immutable purpose-specific pin.
type SSHExecutionRepository interface {
	// BeginSSHExecution spends the original lease's sole dispatch credit before returning private execution coordinates.
	// BeginSSHExecution расходует sole dispatch credit original lease до выдачи private execution coordinates.
	BeginSSHExecution(context.Context, value.Principal, managedssh.Binding) (managedssh.ExecutionAdmission, error)
	// CompleteSSHExecution stores the immutable observation and publishes only under the same fresh owner graph transaction.
	// CompleteSSHExecution сохраняет immutable observation и выдаёт результат только в той же fresh owner graph transaction.
	CompleteSSHExecution(context.Context, value.Principal, command.SSHExecutionComplete) (entity.SSHExecutionReceipt, error)
	// GetSSHExecution returns original recorded metadata without reissuing admission or publishing private output.
	// GetSSHExecution возвращает original recorded metadata без повторной выдачи admission либо публикации private output.
	GetSSHExecution(context.Context, value.Principal, managedssh.Binding) (entity.SSHExecutionReceipt, error)
}
