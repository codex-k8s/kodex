package postgresinbox

import "context"

// Operation is a closed low-cardinality technical activity name for traces and metrics.
// Operation — закрытое имя технического действия низкой кардинальности для трассировки и метрик.
type Operation string

const (
	// OperationProcess covers delivery intake through its durable broker decision.
	// OperationProcess охватывает приём доставки до устойчивого решения для брокера.
	OperationProcess Operation = "process"
	// OperationClaim records an attempt to acquire one fenced event lease.
	// OperationClaim фиксирует попытку получить одну ограждённую аренду события.
	OperationClaim Operation = "claim"
	// OperationApply records execution and durable finalization of a handler effect.
	// OperationApply фиксирует выполнение эффекта обработчика и его устойчивое завершение.
	OperationApply Operation = "apply"
	// OperationCheck measures exact schema and runtime-principal readiness.
	// OperationCheck измеряет готовность точной схемы и рабочего пути учётной записи.
	OperationCheck Operation = "check"
	// OperationRenew measures extension of an exact active lease.
	// OperationRenew измеряет продление точной действующей аренды.
	OperationRenew Operation = "renew"
	// OperationRepair measures an authorized dead-letter requeue.
	// OperationRepair измеряет авторизованный повторный запуск dead-letter.
	OperationRepair Operation = "repair"
	// OperationRecover measures an authorized durable recovery decision.
	// OperationRecover измеряет авторизованное устойчивое решение восстановления.
	OperationRecover Operation = "recover"
	// OperationDelivery measures authoritative readback of one delivery outcome.
	// OperationDelivery измеряет авторитетное чтение результата одной доставки.
	OperationDelivery Operation = "delivery_outcome"
	// OperationList measures bounded operator inspection of blocking events.
	// OperationList измеряет ограниченный просмотр блокирующих событий оператором.
	OperationList Operation = "list"
	// OperationCleanup measures bounded removal of expired terminal rows.
	// OperationCleanup измеряет ограниченное удаление завершённых строк по сроку хранения.
	OperationCleanup Operation = "cleanup"
)

// Observer receives only closed operation and outcome values, never event payload or identifiers.
// Observer получает только закрытые значения операции и исхода, без содержимого и идентификаторов события.
type Observer interface {
	// Observe records one completed operation with its classified outcome.
	// Observe записывает одну завершённую операцию с классифицированным исходом.
	Observe(Operation, Outcome)
}

// Span closes a trace using a classified outcome without exposing event data.
// Span завершает трассировку классифицированным исходом без раскрытия данных события.
type Span interface {
	// End reports the closed terminal outcome to the span implementation.
	// End передаёт закрытый итоговый исход реализации span.
	End(Outcome)
}

// Tracer starts spans only for the closed technical operation vocabulary.
// Tracer начинает spans только для закрытого словаря технических операций.
type Tracer interface {
	// Start returns a derived context and span for one classified operation.
	// Start возвращает производный контекст и span одной классифицированной операции.
	Start(context.Context, Operation) (context.Context, Span)
}

// noopObserver discards observations when no external observer is configured.
// noopObserver отбрасывает наблюдения без настроенного внешнего наблюдателя.
type noopObserver struct{}

// Observe intentionally emits no metrics or event details.
// Observe намеренно не выдаёт метрики или сведения о событии.
func (noopObserver) Observe(Operation, Outcome) {}

// noopTracer preserves the context and returns a span with no side effects.
// noopTracer сохраняет контекст и возвращает span без побочных действий.
type noopTracer struct{}

// Start avoids external trace work when tracing is disabled.
// Start не запускает внешнюю трассировку, когда она выключена.
func (noopTracer) Start(ctx context.Context, _ Operation) (context.Context, Span) {
	return ctx, noopSpan{}
}

// noopSpan fulfills the span contract without emitting telemetry.
// noopSpan выполняет контракт span без отправки телеметрии.
type noopSpan struct{}

// End discards the classified outcome when tracing is disabled.
// End отбрасывает классифицированный исход при выключенной трассировке.
func (noopSpan) End(Outcome) {}

// denyOperatorAuthorizer is the fail-closed default for privileged inbox reads and writes.
// denyOperatorAuthorizer — закрытый по умолчанию отказ для привилегированных чтений и изменений inbox.
type denyOperatorAuthorizer struct{}

// AuthorizeOperator rejects every target until a trusted authorizer is explicitly installed.
// AuthorizeOperator отклоняет каждую цель до явного подключения доверенного авторизатора.
func (denyOperatorAuthorizer) AuthorizeOperator(
	context.Context,
	OperatorTarget,
) (OperatorAuthority, error) {
	return OperatorAuthority{}, ErrOperatorNotAllowed
}

// Option configures processor observers, trusted authorization, or registered effects at construction.
// Option настраивает наблюдение, доверенную авторизацию или зарегистрированные эффекты при создании процессора.
type Option func(*Processor)

// WithObserver installs a non-nil observer for closed operation/outcome pairs.
// WithObserver подключает непустой наблюдатель для закрытых пар операции и исхода.
func WithObserver(observer Observer) Option {
	return func(processor *Processor) {
		if observer != nil {
			processor.observer = observer
		}
	}
}

// WithTracer installs a non-nil tracer restricted to closed operation/outcome values.
// WithTracer подключает непустую трассировку только с закрытыми значениями операции и исхода.
func WithTracer(tracer Tracer) Option {
	return func(processor *Processor) {
		if tracer != nil {
			processor.tracer = tracer
		}
	}
}

// WithOperatorAuthorizer installs the trusted boundary for exact read, recovery, and repair.
// WithOperatorAuthorizer подключает доверенную границу точного чтения, восстановления и исправления.
func WithOperatorAuthorizer(authorizer OperatorAuthorizer) Option {
	return func(processor *Processor) {
		if authorizer != nil {
			processor.operatorAuthorizer = authorizer
		}
	}
}

// WithEffectOperations installs only validated service-owned effects or clears the registry on error.
// WithEffectOperations подключает только проверенные эффекты сервиса либо очищает реестр при ошибке.
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
