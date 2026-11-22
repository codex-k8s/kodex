package sshtarget

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// IdentityExecutionOwner retains the original invocation lease and one immutable dispatch receipt; it never receives key bytes or chooses a replacement credential.
// IdentityExecutionOwner сохраняет original invocation lease и один immutable dispatch receipt; key bytes и выбор replacement credential отсутствуют.
type IdentityExecutionOwner interface {
	// BeginSSHExecution commits sole dispatch credit before disclosing private purpose material coordinates.
	// BeginSSHExecution фиксирует sole dispatch credit до раскрытия private purpose material coordinates.
	BeginSSHExecution(context.Context, managedssh.Binding) (managedssh.ExecutionAdmission, error)
	// CompleteSSHExecution reports one joined observation and returns only the actual owner publication decision.
	// CompleteSSHExecution сообщает одно joined observation и возвращает лишь actual owner publication decision.
	CompleteSSHExecution(context.Context, managedssh.Binding, managedssh.ExecutionAdmission, managedssh.ExecutionObservation) (managedssh.ExecutionReceipt, error)
	// GetSSHExecution reads original metadata, preserving unresolved intent and without reissuing admission or private output.
	// GetSSHExecution читает original metadata, сохраняя unresolved intent без повторной выдачи admission либо private output.
	GetSSHExecution(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error)
}

// IdentityNativeExecutor performs one joined finite SSH operation and calls the exact owner pre-dispatch check immediately before every actual Start; it contains no retry or claim loop.
// IdentityNativeExecutor выполняет одну joined finite SSH operation и вызывает exact owner pre-dispatch check перед каждым actual Start; retry и claim loop отсутствуют.
type IdentityNativeExecutor interface {
	// ExecuteSSHIdentity consumes a private admission once within the caller's handler and checks current owner authority without selecting another target or key.
	// ExecuteSSHIdentity однократно расходует private admission внутри caller handler и проверяет current owner authority без выбора другого target либо key.
	ExecuteSSHIdentity(context.Context, managedssh.ExecutionAdmission, []byte, func(context.Context) error) (managedssh.ExecutionObservation, error)
}
