package postgresinbox

import (
	"context"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/eventing"
)

// Processor координирует durable receive/claim/effect и явный lifecycle.
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

// New создаёт Processor без соединений, миграций и фоновых goroutine.
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

// Process выполняет полный provider-neutral inbox flow для одного envelope.
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

	ctx, span := processor.tracer.Start(ctx, OperationProcess)
	outcome := OutcomeError
	defer func() {
		if result.Outcome != "" {
			outcome = result.Outcome
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		}
		span.End(outcome)
		processor.observer.Observe(OperationProcess, outcome)
	}()

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

// Acquire выполняет durable receive+claim для adapter с отдельным renew/apply.
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

// ApplyClaim выполняет effect и finalization только для exact действующего claim.
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
	ctx, span := processor.tracer.Start(ctx, OperationApply)
	outcome := OutcomeError
	defer func() {
		if result.Outcome != "" {
			outcome = result.Outcome
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		}
		span.End(outcome)
		processor.observer.Observe(OperationApply, outcome)
	}()
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

// Cancel прекращает приём новых операций; in-flight завершаются по caller context.
func (processor *Processor) Cancel() {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	processor.stopping = true
	if processor.inFlight == 0 {
		processor.closeJoined()
	}
}

// Join ожидает завершения in-flight без запуска вспомогательной goroutine.
func (processor *Processor) Join(ctx context.Context) error {
	select {
	case <-processor.joined:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (processor *Processor) enter() error {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	if processor.stopping {
		return ErrProcessorStopped
	}
	processor.inFlight++
	return nil
}

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

func (processor *Processor) closeJoined() {
	processor.joinOnce.Do(func() { close(processor.joined) })
}
