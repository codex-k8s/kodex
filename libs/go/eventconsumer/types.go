package eventconsumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
)

var (
	// ErrRetryable is returned by RunOnce when processing should be retried after backoff.
	ErrRetryable = errors.New("event consumer retryable failure")
	// ErrInvalidConfig reports unsafe process-level consumer settings.
	ErrInvalidConfig = errors.New("invalid event consumer config")
	// ErrDuplicateHandler reports a repeated event type/version registration.
	ErrDuplicateHandler = errors.New("duplicate event consumer handler")
)

// Store is the event-log lease/checkpoint contract used by Runner.
type Store interface {
	Claim(context.Context, eventlog.ClaimParams) (eventlog.ClaimedBatch, error)
	Advance(context.Context, eventlog.AdvanceParams) error
	Release(context.Context, eventlog.ReleaseParams) error
}

// Event is one platform-event-log record passed to a typed handler.
type Event struct {
	StoredEvent eventlog.StoredEvent
	Attempt     int
}

// Handler processes one event and leaves business state changes to the owner service.
type Handler interface {
	HandleEvent(context.Context, Event) Result
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(context.Context, Event) Result

// HandleEvent implements Handler.
func (fn HandlerFunc) HandleEvent(ctx context.Context, event Event) Result {
	if fn == nil {
		return Poison("missing_handler", "event handler is not configured")
	}
	return fn(ctx, event)
}

// ResultStatus describes the checkpoint outcome for one event.
type ResultStatus string

const (
	// ResultAck advances the checkpoint after the handler has idempotently handled the event.
	ResultAck ResultStatus = "ack"
	// ResultRetry releases the lease and keeps the checkpoint before this event.
	ResultRetry ResultStatus = "retry"
	// ResultPoison advances the checkpoint with a bounded safe diagnostic.
	ResultPoison ResultStatus = "poison"
)

// Result is the bounded processing outcome for one event.
type Result struct {
	Status  ResultStatus
	Code    string
	Summary string
	Err     error
}

// Ack returns a successful idempotent event result.
func Ack() Result {
	return Result{Status: ResultAck}
}

// Retry returns a retryable event result.
func Retry(err error) Result {
	if err == nil {
		err = ErrRetryable
	}
	return Result{Status: ResultRetry, Code: "retryable", Summary: "event processing will be retried", Err: err}
}

// Poison returns a permanent safe event result.
func Poison(code string, summary string) Result {
	return Result{Status: ResultPoison, Code: safeToken(code, "poison"), Summary: safeSummary(summary, 256)}
}

// Config controls event-log claim, checkpoint and handler pacing.
type Config struct {
	ConsumerName        string
	LeaseOwner          string
	BatchSize           int
	PollInterval        time.Duration
	LeaseTTL            time.Duration
	HandlerTimeout      time.Duration
	RetryInitialDelay   time.Duration
	RetryMaxDelay       time.Duration
	FailureMessageLimit int
	ConcurrencyLimit    int
	MaxAttempts         int
}

// ConfigFromRuntimeValues converts service env fields to consumer runtime config.
func ConfigFromRuntimeValues(
	consumerName string,
	leaseOwner string,
	batchSize int,
	pollInterval time.Duration,
	leaseTTL time.Duration,
	handlerTimeout time.Duration,
	retryInitialDelay time.Duration,
	retryMaxDelay time.Duration,
	failureMessageLimit int,
	concurrencyLimit int,
	maxAttempts int,
) Config {
	return Config{
		ConsumerName:        consumerName,
		LeaseOwner:          leaseOwner,
		BatchSize:           batchSize,
		PollInterval:        pollInterval,
		LeaseTTL:            leaseTTL,
		HandlerTimeout:      handlerTimeout,
		RetryInitialDelay:   retryInitialDelay,
		RetryMaxDelay:       retryMaxDelay,
		FailureMessageLimit: failureMessageLimit,
		ConcurrencyLimit:    concurrencyLimit,
		MaxAttempts:         maxAttempts,
	}
}

func (cfg Config) validate() error {
	switch {
	case strings.TrimSpace(cfg.ConsumerName) == "":
		return fmt.Errorf("%w: consumer name is required", ErrInvalidConfig)
	case strings.TrimSpace(cfg.LeaseOwner) == "":
		return fmt.Errorf("%w: lease owner is required", ErrInvalidConfig)
	case cfg.BatchSize < 1:
		return fmt.Errorf("%w: batch size must be positive", ErrInvalidConfig)
	case cfg.PollInterval <= 0:
		return fmt.Errorf("%w: poll interval must be positive", ErrInvalidConfig)
	case cfg.LeaseTTL <= 0:
		return fmt.Errorf("%w: lease ttl must be positive", ErrInvalidConfig)
	case cfg.HandlerTimeout <= 0:
		return fmt.Errorf("%w: handler timeout must be positive", ErrInvalidConfig)
	case cfg.RetryInitialDelay <= 0:
		return fmt.Errorf("%w: retry initial delay must be positive", ErrInvalidConfig)
	case cfg.RetryMaxDelay < cfg.RetryInitialDelay:
		return fmt.Errorf("%w: retry max delay must be greater than or equal to initial delay", ErrInvalidConfig)
	case cfg.FailureMessageLimit < 1:
		return fmt.Errorf("%w: failure message limit must be positive", ErrInvalidConfig)
	case cfg.ConcurrencyLimit < 1:
		return fmt.Errorf("%w: concurrency limit must be positive", ErrInvalidConfig)
	case cfg.MaxAttempts < 1:
		return fmt.Errorf("%w: max attempts must be positive", ErrInvalidConfig)
	default:
		return nil
	}
}

// DefaultLeaseOwner creates a bounded best-effort lease owner for one process.
func DefaultLeaseOwner(serviceName string) string {
	serviceName = safeToken(serviceName, "service")
	host := "host"
	if name, err := os.Hostname(); err == nil {
		host = safeToken(name, "host")
	}
	return serviceName + "@" + host
}

// Hook receives safe consumer lifecycle diagnostics.
type Hook interface {
	Claimed(context.Context, ClaimInfo)
	Handled(context.Context, HandleInfo)
}

// ClaimInfo describes one safe event-log claim outcome.
type ClaimInfo struct {
	ConsumerName string
	LeaseOwner   string
	EventCount   int
}

// HandleInfo describes one bounded handler outcome.
type HandleInfo struct {
	ConsumerName  string
	EventSequence int64
	EventType     string
	SchemaVersion int
	Status        ResultStatus
	Code          string
	Summary       string
	Attempt       int
}

type noopHook struct{}

func (noopHook) Claimed(context.Context, ClaimInfo)  {}
func (noopHook) Handled(context.Context, HandleInfo) {}

func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
