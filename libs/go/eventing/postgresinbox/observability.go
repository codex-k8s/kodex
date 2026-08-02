package postgresinbox

import "context"

// Operation — закрытое имя технической операции.
type Operation string

const (
	OperationProcess  Operation = "process"
	OperationClaim    Operation = "claim"
	OperationApply    Operation = "apply"
	OperationCheck    Operation = "check"
	OperationRenew    Operation = "renew"
	OperationRepair   Operation = "repair"
	OperationRecover  Operation = "recover"
	OperationDelivery Operation = "delivery_outcome"
	OperationList     Operation = "list"
	OperationCleanup  Operation = "cleanup"
)

// Observer получает только закрытые низкокардинальные значения.
type Observer interface {
	Observe(Operation, Outcome)
}

// Span завершает trace закрытым outcome без payload и identifiers.
type Span interface {
	End(Outcome)
}

// Tracer создаёт span только по закрытому имени operation.
type Tracer interface {
	Start(context.Context, Operation) (context.Context, Span)
}

type noopObserver struct{}

func (noopObserver) Observe(Operation, Outcome) {}

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ Operation) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End(Outcome) {}

type denyOperatorAuthorizer struct{}

func (denyOperatorAuthorizer) AuthorizeOperator(
	context.Context,
	OperatorTarget,
) (OperatorAuthority, error) {
	return OperatorAuthority{}, ErrOperatorNotAllowed
}

// Option изменяет только provider-neutral hooks Processor.
type Option func(*Processor)

// WithObserver подключает bounded observer.
func WithObserver(observer Observer) Option {
	return func(processor *Processor) {
		if observer != nil {
			processor.observer = observer
		}
	}
}

// WithTracer подключает tracer с закрытыми operation/outcome.
func WithTracer(tracer Tracer) Option {
	return func(processor *Processor) {
		if tracer != nil {
			processor.tracer = tracer
		}
	}
}

// WithOperatorAuthorizer подключает trusted boundary read/recovery/repair.
func WithOperatorAuthorizer(authorizer OperatorAuthorizer) Option {
	return func(processor *Processor) {
		if authorizer != nil {
			processor.operatorAuthorizer = authorizer
		}
	}
}

// WithEffectOperations задаёт закрытый реестр service-owned effect functions.
func WithEffectOperations(operations ...EffectOperation) Option {
	return func(processor *Processor) {
		registered, err := buildEffectOperations(operations)
		if err != nil {
			processor.effectOperations = nil
			return
		}
		processor.effectOperations = registered
	}
}
