package eventconsumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
)

var errHandlerPanic = errors.New("event handler panicked")

// Runner claims platform-event-log events and dispatches them to typed handlers.
type Runner struct {
	store    Store
	registry Registry
	cfg      Config
	logger   *slog.Logger
	hook     Hook
}

// NewRunner creates a shared platform-event-log consumer runtime.
func NewRunner(store Store, registry Registry, cfg Config, logger *slog.Logger, hook Hook) (*Runner, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidConfig)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if hook == nil {
		hook = noopHook{}
	}
	return &Runner{
		store:    store,
		registry: registry,
		cfg:      cfg,
		logger:   loggerOrDefault(logger),
		hook:     hook,
	}, nil
}

// Run processes events until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logRetryable(ctx, err)
	}
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logRetryable(ctx, err)
				delay := r.retryDelay(1)
				if err := sleepContext(ctx, delay); err != nil {
					return nil
				}
			}
		}
	}
}

// RunOnce claims and processes at most one event-log batch.
func (r *Runner) RunOnce(ctx context.Context) error {
	now := time.Now().UTC()
	batch, err := r.store.Claim(ctx, eventlog.ClaimParams{
		ConsumerName: r.cfg.ConsumerName,
		LeaseOwner:   r.cfg.LeaseOwner,
		Limit:        r.cfg.BatchSize,
		Now:          now,
		LockedUntil:  now.Add(r.cfg.LeaseTTL),
	})
	if err != nil {
		return fmt.Errorf("claim event log batch: %w", err)
	}
	r.hook.Claimed(ctx, ClaimInfo{ConsumerName: r.cfg.ConsumerName, LeaseOwner: r.cfg.LeaseOwner, EventCount: len(batch.Events)})
	if len(batch.Events) == 0 {
		return nil
	}
	outcomes := r.handleBatch(ctx, batch)
	advanceTo, retryEvent, retryOutcome := r.contiguousAdvance(batch.Events, outcomes)
	if retryEvent.SequenceID != 0 {
		if err := r.deferRetry(ctx, batch, retryEvent, retryOutcome); err != nil {
			return err
		}
		return fmt.Errorf("%w: event sequence %d: %s", ErrRetryable, retryEvent.SequenceID, safeSummary(errorText(retryOutcome.result), r.cfg.FailureMessageLimit))
	}
	if advanceTo > 0 {
		if err := r.store.Advance(ctx, eventlog.AdvanceParams{
			ConsumerName:   batch.ConsumerName,
			LeaseOwner:     batch.LeaseOwner,
			LastSequenceID: advanceTo,
			Now:            time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("advance event log checkpoint: %w", err)
		}
	}
	return nil
}

func (r *Runner) handleBatch(ctx context.Context, batch eventlog.ClaimedBatch) []eventOutcome {
	events := batch.Events
	outcomes := make([]eventOutcome, len(events))
	workerLimit := r.cfg.ConcurrencyLimit
	if workerLimit > len(events) {
		workerLimit = len(events)
	}
	sem := make(chan struct{}, workerLimit)
	var wg sync.WaitGroup
	for index, storedEvent := range events {
		attempt := eventAttempt(batch, storedEvent)
		if ctx.Err() != nil {
			outcomes[index] = eventOutcome{result: Retry(ctx.Err()), attempt: attempt}
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(index int, storedEvent eventlog.StoredEvent, attempt int) {
			defer wg.Done()
			defer func() { <-sem }()
			outcomes[index] = eventOutcome{result: r.handleOne(ctx, storedEvent, attempt), attempt: attempt}
		}(index, storedEvent, attempt)
	}
	wg.Wait()
	return outcomes
}

func (r *Runner) handleOne(ctx context.Context, storedEvent eventlog.StoredEvent, attempt int) Result {
	handler, status := r.registry.lookup(storedEvent)
	var result Result
	switch status {
	case lookupHandled:
		handlerCtx, cancel := context.WithTimeout(ctx, r.cfg.HandlerTimeout)
		result = r.callHandler(handlerCtx, handler, Event{StoredEvent: storedEvent, Attempt: attempt})
		handlerErr := handlerCtx.Err()
		cancel()
		if handlerErr != nil {
			result = Retry(handlerErr)
		}
	case lookupUnsupportedVersion:
		result = Poison("unsupported_schema_version", "event schema version is not registered for this consumer")
	default:
		result = Ack()
	}
	result = r.normalizeResult(storedEvent, attempt, result)
	r.hook.Handled(ctx, HandleInfo{
		ConsumerName:  r.cfg.ConsumerName,
		EventSequence: storedEvent.SequenceID,
		EventType:     storedEvent.EventType,
		SchemaVersion: storedEvent.SchemaVersion,
		Status:        result.Status,
		Code:          result.Code,
		Summary:       result.Summary,
		Attempt:       attempt,
	})
	r.logHandled(storedEvent, result, attempt)
	return result
}

type eventOutcome struct {
	result  Result
	attempt int
}

func (r *Runner) callHandler(ctx context.Context, handler Handler, event Event) (result Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Result{
				Status:  ResultRetry,
				Code:    "handler_panic",
				Summary: "event handler panicked",
				Err:     errHandlerPanic,
			}
		}
	}()
	return handler.HandleEvent(ctx, event)
}

func (r *Runner) normalizeResult(_ eventlog.StoredEvent, attempt int, result Result) Result {
	if result.Status == "" {
		result = Retry(errors.New("event handler returned empty status"))
	}
	if result.Status == ResultRetry && attempt >= r.cfg.MaxAttempts {
		result = Poison("max_attempts_exceeded", "event retry limit exceeded")
	}
	result.Code = safeToken(result.Code, string(result.Status))
	result.Summary = safeSummary(firstNonEmpty(result.Summary, errorText(result)), r.cfg.FailureMessageLimit)
	return result
}

func (r *Runner) contiguousAdvance(events []eventlog.StoredEvent, outcomes []eventOutcome) (int64, eventlog.StoredEvent, eventOutcome) {
	var advanceTo int64
	for index, outcome := range outcomes {
		if outcome.result.Status == ResultRetry {
			return advanceTo, events[index], outcome
		}
		advanceTo = events[index].SequenceID
	}
	return advanceTo, eventlog.StoredEvent{}, eventOutcome{}
}

func (r *Runner) deferRetry(ctx context.Context, batch eventlog.ClaimedBatch, storedEvent eventlog.StoredEvent, outcome eventOutcome) error {
	now := time.Now().UTC()
	if err := r.store.Defer(ctx, eventlog.DeferParams{
		ConsumerName:    batch.ConsumerName,
		LeaseOwner:      batch.LeaseOwner,
		RetrySequenceID: storedEvent.SequenceID,
		RetryAttempt:    outcome.attempt,
		LastError:       safeSummary(errorText(outcome.result), r.cfg.FailureMessageLimit),
		Now:             now,
		LockedUntil:     now.Add(r.retryDelay(outcome.attempt)),
	}); err != nil {
		return fmt.Errorf("defer event log checkpoint retry: %w", err)
	}
	return nil
}

func eventAttempt(batch eventlog.ClaimedBatch, storedEvent eventlog.StoredEvent) int {
	if batch.RetrySequenceID == storedEvent.SequenceID && batch.RetryAttempt > 0 {
		return batch.RetryAttempt + 1
	}
	return 1
}

func (r *Runner) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := r.cfg.RetryInitialDelay
	for remaining := attempt - 1; remaining > 0; remaining-- {
		next := delay + delay
		if next <= delay || next >= r.cfg.RetryMaxDelay {
			delay = r.cfg.RetryMaxDelay
			break
		}
		delay = next
	}
	return delay
}

func (r *Runner) logRetryable(_ context.Context, err error) {
	r.logger.Warn("event consumer batch will be retried", "consumer_name", r.cfg.ConsumerName, "error", safeSummary(err.Error(), r.cfg.FailureMessageLimit))
}

func (r *Runner) logHandled(storedEvent eventlog.StoredEvent, result Result, attempt int) {
	args := []any{
		"consumer_name", r.cfg.ConsumerName,
		"event_sequence", storedEvent.SequenceID,
		"event_type", storedEvent.EventType,
		"schema_version", storedEvent.SchemaVersion,
		"status", string(result.Status),
		"attempt", attempt,
	}
	if result.Code != "" {
		args = append(args, "code", result.Code)
	}
	if result.Summary != "" {
		args = append(args, "summary", result.Summary)
	}
	switch result.Status {
	case ResultRetry:
		r.logger.Warn("event consumer handler retry requested", args...)
	case ResultPoison:
		r.logger.Error("event consumer handler poisoned event", args...)
	default:
		r.logger.Info("event consumer handler acknowledged event", args...)
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func errorText(result Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	return result.Summary
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
