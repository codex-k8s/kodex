package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/jackc/pgx/v5"
)

const (
	maximumConsumerLength       = 128
	maximumSchemaLength         = 63
	maximumInstanceLength       = 128
	maximumErrorCodeLength      = 63
	maximumIdempotencyKeyLength = 128
	maximumActorLength          = 256
	maximumReasonLength         = 1024
	minimumIdempotencyKeyLength = 8
	minimumRetentionHorizon     = 24 * time.Hour
	maximumRetentionHorizon     = 10 * 365 * 24 * time.Hour
	maximumLeaseDuration        = 15 * time.Minute
	maximumBackoff              = 24 * time.Hour
	maximumCleanupBatch         = 1000
	maximumTransactionRetries   = 3
	schemaVersion               = 1
	schemaComponent             = "postgresinbox"
	schemaDigestHex             = "d5d672cab4214cd25d25e97300410e9fd42fcfa9ec797dfe4ea0ec2eec8e6e44"
)

var (
	consumerNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	consumerScopePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	schemaNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	instanceIDPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	errorCodePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	repairKeyPattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,127}$`)
)

// Beginner открывает принадлежащую Processor PostgreSQL-транзакцию.
type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// Handler выполняет только локальный effect через переданную транзакцию.
type Handler func(context.Context, pgx.Tx, eventing.Envelope) error

// Consumer задаёт назначенную сервером устойчивую identity consumer.
type Consumer struct {
	Name  string
	Scope string
}

func (consumer Consumer) validate() error {
	if len(consumer.Name) == 0 || len(consumer.Name) > maximumConsumerLength ||
		!consumerNamePattern.MatchString(consumer.Name) ||
		len(consumer.Scope) == 0 || len(consumer.Scope) > maximumConsumerLength ||
		!consumerScopePattern.MatchString(consumer.Scope) {
		return ErrInvalidConsumer
	}
	return nil
}

// Config задаёт bounded runtime contract Processor.
type Config struct {
	Schema           string
	InstanceID       string
	LeaseDuration    time.Duration
	EffectTimeout    time.Duration
	FinalizeTimeout  time.Duration
	InitialBackoff   time.Duration
	MaximumBackoff   time.Duration
	MaxAttempts      uint32
	MaxRepairs       uint32
	RetentionHorizon time.Duration
	CleanupBatchSize int
}

func (config Config) withDefaults() Config {
	if config.FinalizeTimeout == 0 {
		config.FinalizeTimeout = 5 * time.Second
	}
	if config.CleanupBatchSize == 0 {
		config.CleanupBatchSize = 100
	}
	return config
}

func (config Config) validate() error {
	if len(config.Schema) == 0 || len(config.Schema) > maximumSchemaLength ||
		!schemaNamePattern.MatchString(config.Schema) ||
		strings.HasPrefix(config.Schema, "pg_") || config.Schema == "information_schema" ||
		len(config.InstanceID) == 0 || len(config.InstanceID) > maximumInstanceLength ||
		!instanceIDPattern.MatchString(config.InstanceID) ||
		config.LeaseDuration < time.Second ||
		config.LeaseDuration > maximumLeaseDuration ||
		config.EffectTimeout <= 0 ||
		config.FinalizeTimeout <= 0 ||
		config.EffectTimeout+config.FinalizeTimeout >= config.LeaseDuration ||
		config.InitialBackoff < 100*time.Millisecond ||
		config.MaximumBackoff < config.InitialBackoff ||
		config.MaximumBackoff > maximumBackoff ||
		config.MaxAttempts < 1 || config.MaxAttempts > 100 ||
		config.MaxRepairs < 1 || config.MaxRepairs > 20 ||
		config.RetentionHorizon < minimumRetentionHorizon ||
		config.RetentionHorizon > maximumRetentionHorizon ||
		config.CleanupBatchSize < 1 || config.CleanupBatchSize > maximumCleanupBatch {
		return ErrInvalidConfiguration
	}
	return nil
}

// BrokerAction — закрытое решение для provider-specific broker adapter.
type BrokerAction string

const (
	BrokerActionACK          BrokerAction = "ack"
	BrokerActionNACKRetry    BrokerAction = "nack_retry"
	BrokerActionNACKTerminal BrokerAction = "nack_terminal"
)

// Outcome — закрытый низкокардинальный исход операции inbox.
type Outcome string

const (
	OutcomeProcessed  Outcome = "processed"
	OutcomeClaimed    Outcome = "claimed"
	OutcomeRenewed    Outcome = "renewed"
	OutcomeDuplicate  Outcome = "duplicate"
	OutcomeStale      Outcome = "stale"
	OutcomeGap        Outcome = "gap"
	OutcomeBusy       Outcome = "busy"
	OutcomeRetry      Outcome = "retry"
	OutcomeDeadLetter Outcome = "dead_letter"
	OutcomeConflict   Outcome = "conflict"
	OutcomeRepaired   Outcome = "repaired"
	OutcomeCleaned    Outcome = "cleaned"
	OutcomeReady      Outcome = "ready"
	OutcomeCanceled   Outcome = "canceled"
	OutcomeError      Outcome = "error"
)

// Result разрешено преобразовывать в broker action только при Durable=true.
type Result struct {
	Outcome Outcome
	Action  BrokerAction
	Durable bool
}

// Claim связывает exact event с назначенными lease, generation и fence.
type Claim struct {
	Consumer        Consumer
	EventID         string
	EventDigest     [sha256.Size]byte
	OrderingKey     string
	EventSequence   uint64
	LeaseOwner      string
	LeaseToken      string
	LeaseGeneration uint64
	LeaseFence      uint64
	LeaseExpiresAt  time.Time
	Attempts        uint32
	MaxAttempts     uint32
}

// EffectFailure классифицирует handler failure без утечки исходного текста.
type EffectFailure struct {
	code      string
	retryable bool
	cause     error
}

// NewEffectFailure создаёт безопасно классифицированную ошибку effect.
func NewEffectFailure(code string, retryable bool, cause error) error {
	if cause == nil {
		cause = ErrEffectFailed
	}
	if len(code) == 0 || len(code) > maximumErrorCodeLength ||
		!errorCodePattern.MatchString(code) {
		code = errorCodeEffectFailed
	}
	return &EffectFailure{code: code, retryable: retryable, cause: cause}
}

func (failure *EffectFailure) Error() string { return errorTextEffectFailed }
func (failure *EffectFailure) Unwrap() error { return failure.cause }

// Code возвращает bounded persisted failure code.
func (failure *EffectFailure) Code() string { return failure.code }

// Retryable сообщает, разрешён ли следующий attempt.
func (failure *EffectFailure) Retryable() bool { return failure.retryable }

// RepairRequest задаёт bounded audited REQUEUE exact dead-letter predecessor.
type RepairRequest struct {
	Consumer           Consumer
	IdempotencyKey     string
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
	Reason             string
	EvidenceDigest     [sha256.Size]byte
}

func (request RepairRequest) validate() error {
	if err := request.Consumer.validate(); err != nil {
		return err
	}
	if len(request.IdempotencyKey) < minimumIdempotencyKeyLength ||
		len(request.IdempotencyKey) > maximumIdempotencyKeyLength ||
		!repairKeyPattern.MatchString(request.IdempotencyKey) ||
		len(request.Reason) == 0 || len(request.Reason) > maximumReasonLength ||
		strings.TrimSpace(request.Reason) != request.Reason ||
		request.ExpectedGeneration == 0 || request.ExpectedFence == 0 {
		return ErrInvalidRepair
	}
	if !canonicalUUID(request.EventID) ||
		request.EventDigest == ([sha256.Size]byte{}) ||
		request.EvidenceDigest == ([sha256.Size]byte{}) ||
		request.ExpectedGeneration > math.MaxInt64 ||
		request.ExpectedFence > math.MaxInt64 {
		return ErrInvalidRepair
	}
	return nil
}

// RepairTarget передаёт authorizer только exact immutable repair coordinates.
type RepairTarget struct {
	Consumer           Consumer
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
}

// RepairAuthority содержит actor, разрешённого внешней trusted boundary.
type RepairAuthority struct {
	Actor string
}

// RepairAuthorizer разрешает actor из context/authoritative state, не из request.
type RepairAuthorizer interface {
	AuthorizeRepair(context.Context, RepairTarget) (RepairAuthority, error)
}

// RepairReceipt является неизменяемым durable результатом repair.
type RepairReceipt struct {
	RepairID        string
	EventID         string
	EventDigest     [sha256.Size]byte
	Generation      uint64
	Fence           uint64
	CreatedAt       time.Time
	AlreadyRepaired bool
}

func classifyEffectFailure(err error) *EffectFailure {
	var classified *EffectFailure
	if errors.As(err, &classified) {
		return classified
	}
	return &EffectFailure{
		code:      errorCodeEffectFailed,
		retryable: true,
		cause:     err,
	}
}
