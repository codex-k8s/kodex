package postgresinbox

import (
	"context"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/eventing"
)

// Processor owns durable receive, claim, effect, and shutdown coordination without background workers.
// Processor управляет устойчивым приёмом, захватом, эффектом и остановкой без фоновых исполнителей.
type Processor struct {
	beginner           Beginner
	config             Config
	queries            querySet
	observer           Observer
	tracer             Tracer
	operatorAuthorizer OperatorAuthorizer
	effectOperations   map[string]EffectOperation

	mu       sync.Mutex
	stopping bool
	inFlight uint64
	joined   chan struct{}
	joinOnce sync.Once
}

// New validates the exact configuration and effect registry without opening connections or running migrations.
// New проверяет точную конфигурацию и реестр эффектов без соединений и миграций.
func New(beginner Beginner, config Config, options ...Option) (*Processor, error) {
	config = config.withDefaults()
	if beginner == nil {
		return nil, ErrInvalidConfiguration
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	queries, err := loadQueries()
	if err != nil {
		return nil, err
	}
	processor := &Processor{
		beginner:           beginner,
		config:             config,
		queries:            queries,
		observer:           noopObserver{},
		tracer:             noopTracer{},
		operatorAuthorizer: denyOperatorAuthorizer{},
		effectOperations:   make(map[string]EffectOperation),
		joined:             make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(processor)
		}
	}
	if processor.effectOperations == nil {
		return nil, ErrInvalidEffectOperation
	}
	for _, operation := range processor.effectOperations {
		if operation.schema != config.Schema {
			return nil, ErrInvalidEffectOperation
		}
	}
	return processor, nil
}

// Process receives, fences, applies, and finalizes one envelope; broker action is returned only with durable evidence.
// Process принимает, ограждает, применяет и завершает конверт; действие брокеру возвращается только с устойчивыми данными.
func (processor *Processor) Process(
	ctx context.Context,
	consumer Consumer,
	envelope eventing.Envelope,
	handler Handler,
) (result Result, err error) {
	if err := processor.enter(); err != nil {
		return Result{}, err
	}
	defer processor.leave()

	ctx, endObservation := processor.observeResultOperation(ctx, OperationProcess, &result, &err)
	defer endObservation()

	if err := consumer.validate(); err != nil {
		return Result{}, err
	}
	if handler == nil {
		return Result{}, ErrEffectFailed
	}
	record, err := newEventRecord(envelope)
	if err != nil {
		return Result{}, err
	}

	received, err := processor.receive(ctx, consumer, record)
	if !received.claimable || err != nil {
		return received.result, err
	}
	claimed, err := processor.claim(ctx, consumer, record)
	if err != nil || claimed.claim == nil {
		return claimed.result, err
	}
	return processor.apply(ctx, record, *claimed.claim, handler)
}

// Acquire durably receives and claims one event for an adapter that renews and applies separately.
// Acquire устойчиво принимает и захватывает событие для адаптера с отдельным продлением и применением.
func (processor *Processor) Acquire(
	ctx context.Context,
	consumer Consumer,
	envelope eventing.Envelope,
) (claim Claim, result Result, err error) {
	if err := processor.enter(); err != nil {
		return Claim{}, Result{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationClaim)
	outcome := OutcomeError
	defer func() {
		if result.Outcome != "" {
			outcome = result.Outcome
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		} else if err == nil {
			outcome = OutcomeClaimed
		}
		span.End(outcome)
		processor.observer.Observe(OperationClaim, outcome)
	}()
	if err := consumer.validate(); err != nil {
		return Claim{}, Result{}, err
	}
	record, err := newEventRecord(envelope)
	if err != nil {
		return Claim{}, Result{}, err
	}
	received, err := processor.receive(ctx, consumer, record)
	if !received.claimable || err != nil {
		return Claim{}, received.result, err
	}
	claimed, err := processor.claim(ctx, consumer, record)
	if err != nil || claimed.claim == nil {
		return Claim{}, claimed.result, err
	}
	return *claimed.claim, Result{}, nil
}

// ApplyClaim checks the exact envelope, digest, sequence, and fenced claim before effect finalization.
// ApplyClaim проверяет точные конверт, дайджест, номер и ограждённый claim до завершения эффекта.
func (processor *Processor) ApplyClaim(
	ctx context.Context,
	claim Claim,
	envelope eventing.Envelope,
	handler Handler,
) (result Result, err error) {
	if err := processor.enter(); err != nil {
		return Result{}, err
	}
	defer processor.leave()
	ctx, endObservation := processor.observeResultOperation(ctx, OperationApply, &result, &err)
	defer endObservation()
	if err := validateClaim(claim); err != nil {
		return Result{}, err
	}
	if handler == nil {
		return Result{}, ErrEffectFailed
	}
	record, err := newEventRecord(envelope)
	if err != nil {
		return Result{}, err
	}
	if claim.EventID != record.Envelope.EventID ||
		claim.EventSequence != record.Envelope.EventSequence ||
		!sameDigest(claim.EventDigest[:], record.Digest[:]) ||
		!sameOrderingKey(claim.OrderingKey, record.OrderingKey) {
		return Result{}, ErrEventConflict
	}
	return processor.apply(ctx, record, claim, handler)
}

// Cancel closes the admission barrier while existing calls finish under their caller contexts.
// Cancel закрывает приём новых операций, пока текущие вызовы завершаются по контекстам вызывающих.
func (processor *Processor) Cancel() {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	processor.stopping = true
	if processor.inFlight == 0 {
		processor.closeJoined()
	}
}

// Join waits for all admitted calls after cancellation without creating a helper goroutine.
// Join ожидает завершения всех принятых вызовов после отмены без вспомогательной goroutine.
func (processor *Processor) Join(ctx context.Context) error {
	select {
	case <-processor.joined:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// enter atomically rejects work after shutdown or counts it as in flight.
// enter атомарно отклоняет работу после остановки либо учитывает её как выполняемую.
func (processor *Processor) enter() error {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	if processor.stopping {
		return ErrProcessorStopped
	}
	processor.inFlight++
	return nil
}

// leave decrements in-flight work and releases Join once shutdown has drained.
// leave уменьшает число текущих операций и освобождает Join после их завершения.
func (processor *Processor) leave() {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	if processor.inFlight > 0 {
		processor.inFlight--
	}
	if processor.stopping && processor.inFlight == 0 {
		processor.closeJoined()
	}
}

// closeJoined closes the completion channel at most once across concurrent exit paths.
// closeJoined закрывает канал завершения не более одного раза при конкурирующих выходах.
func (processor *Processor) closeJoined() {
	processor.joinOnce.Do(func() { close(processor.joined) })
}

// observeResultOperation preserves the process/apply trace outcome precedence at the call's exit.
// observeResultOperation сохраняет порядок выбора исхода трассировки process/apply при завершении вызова.
func (processor *Processor) observeResultOperation(
	ctx context.Context,
	operation Operation,
	result *Result,
	err *error,
) (context.Context, func()) {
	ctx, span := processor.tracer.Start(ctx, operation)
	return ctx, func() {
		outcome := OutcomeError
		if result.Outcome != "" {
			outcome = result.Outcome
		} else if isContextDone(*err) {
			outcome = OutcomeCanceled
		}
		span.End(outcome)
		processor.observer.Observe(operation, outcome)
	}
}
