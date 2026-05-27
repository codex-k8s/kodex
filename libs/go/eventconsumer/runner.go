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

// Runner claims platform-event-log events and dispatches them to typed handlers.
type Runner struct {
	store    Store
	registry Registry
	cfg      Config
	logger   *slog.Logger
	hook     Hook

	mu       sync.Mutex
	attempts map[string]int
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
		attempts: make(map[string]int),
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
				delay := r.retryDelay(r.maxAttempt())
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
	results := r.handleBatch(ctx, batch.Events)
	advanceTo, retryEvent, retryResult := r.contiguousAdvance(batch.Events, results)
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
	if retryEvent.SequenceID == 0 {
		return nil
	}
	if err := r.store.Release(ctx, eventlog.ReleaseParams{
		ConsumerName: batch.ConsumerName,
		LeaseOwner:   batch.LeaseOwner,
		Now:          time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("release event log checkpoint: %w", err)
	}
	return fmt.Errorf("%w: event sequence %d: %s", ErrRetryable, retryEvent.SequenceID, safeSummary(errorText(retryResult), r.cfg.FailureMessageLimit))
}

func (r *Runner) handleBatch(ctx context.Context, events []eventlog.StoredEvent) []Result {
	results := make([]Result, len(events))
	workerLimit := r.cfg.ConcurrencyLimit
	if workerLimit > len(events) {
		workerLimit = len(events)
	}
	sem := make(chan struct{}, workerLimit)
	var wg sync.WaitGroup
	for index, storedEvent := range events {
		if ctx.Err() != nil {
			results[index] = Retry(ctx.Err())
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(index int, storedEvent eventlog.StoredEvent) {
			defer wg.Done()
			defer func() { <-sem }()
			results[index] = r.handleOne(ctx, storedEvent)
		}(index, storedEvent)
	}
	wg.Wait()
	return results
}

func (r *Runner) handleOne(ctx context.Context, storedEvent eventlog.StoredEvent) Result {
	attempt := r.nextAttempt(storedEvent)
	handler, status := r.registry.lookup(storedEvent)
	var result Result
	switch status {
	case lookupHandled:
		handlerCtx, cancel := context.WithTimeout(ctx, r.cfg.HandlerTimeout)
		result = handler.HandleEvent(handlerCtx, Event{StoredEvent: storedEvent, Attempt: attempt})
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

func (r *Runner) normalizeResult(storedEvent eventlog.StoredEvent, attempt int, result Result) Result {
	if result.Status == "" {
		result = Retry(errors.New("event handler returned empty status"))
	}
	if result.Status == ResultRetry && attempt >= r.cfg.MaxAttempts {
		result = Poison("max_attempts_exceeded", "event retry limit exceeded")
	}
	result.Code = safeToken(result.Code, string(result.Status))
	result.Summary = safeSummary(firstNonEmpty(result.Summary, errorText(result)), r.cfg.FailureMessageLimit)
	if result.Status == ResultAck || result.Status == ResultPoison {
		r.clearAttempt(storedEvent)
	}
	return result
}

func (r *Runner) contiguousAdvance(events []eventlog.StoredEvent, results []Result) (int64, eventlog.StoredEvent, Result) {
	var advanceTo int64
	for index, result := range results {
		if result.Status == ResultRetry {
			return advanceTo, events[index], result
		}
		advanceTo = events[index].SequenceID
	}
	return advanceTo, eventlog.StoredEvent{}, Result{}
}

func (r *Runner) nextAttempt(storedEvent eventlog.StoredEvent) int {
	key := storedEvent.ID.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	attempt := r.attempts[key] + 1
	r.attempts[key] = attempt
	return attempt
}

func (r *Runner) clearAttempt(storedEvent eventlog.StoredEvent) {
	key := storedEvent.ID.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, key)
}

func (r *Runner) maxAttempt() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	maxAttempt := 1
	for _, attempt := range r.attempts {
		if attempt > maxAttempt {
			maxAttempt = attempt
		}
	}
	return maxAttempt
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
