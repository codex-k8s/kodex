package postgresinbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	maximumBlockagePage         = 100
	defaultBlockagePage         = 50
	schemaVersion               = 1
	schemaComponent             = "postgresinbox"
	schemaDigestHex             = "4c44aeb7b45033cd140b9d49db24d67d0ff620687249879d3274427e1e29d5f2"
)

var (
	consumerNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	consumerScopePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	schemaNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	instanceIDPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	errorCodePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	repairKeyPattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,127}$`)
	effectNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// Beginner открывает принадлежащую Processor PostgreSQL-транзакцию.
type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// EventSnapshot хранит закреплённую копию envelope без разделяемой mutable памяти.
type EventSnapshot struct {
	envelope eventing.Envelope
}

// Envelope возвращает независимую копию закреплённого envelope.
func (snapshot EventSnapshot) Envelope() eventing.Envelope {
	return cloneEnvelope(snapshot.envelope)
}

// Data возвращает независимую копию закреплённого payload.
func (snapshot EventSnapshot) Data() json.RawMessage {
	return append(json.RawMessage(nil), snapshot.envelope.Data...)
}

// EffectOperation — opaque ссылка на зарегистрированную service-owned функцию.
type EffectOperation struct {
	name     string
	schema   string
	function string
	query    string
}

// NewEffectOperation создаёт вызов exact schema-qualified функции (jsonb)->jsonb.
func NewEffectOperation(name, schema, function string) (EffectOperation, error) {
	if !effectNamePattern.MatchString(name) ||
		!schemaNamePattern.MatchString(schema) || strings.HasPrefix(schema, "pg_") ||
		!effectNamePattern.MatchString(function) {
		return EffectOperation{}, ErrInvalidEffectOperation
	}
	query, err := buildEffectCallQuery(pgx.Identifier{schema, function}.Sanitize())
	if err != nil {
		return EffectOperation{}, err
	}
	return EffectOperation{
		name:     name,
		schema:   schema,
		function: function,
		query:    query,
	}, nil
}

// Name возвращает bounded техническое имя операции.
func (operation EffectOperation) Name() string { return operation.name }

// EffectTx не раскрывает соединение, transaction/session control или raw SQL.
type EffectTx interface {
	Call(EffectOperation, json.RawMessage) (json.RawMessage, error)
}

// Handler выполняет локальный effect через узкую transaction-bound capability.
type Handler func(context.Context, EffectTx, EventSnapshot) error

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
		config.EffectTimeout <= 0 || config.EffectTimeout >= config.LeaseDuration ||
		config.FinalizeTimeout <= 0 ||
		config.FinalizeTimeout >= config.LeaseDuration-config.EffectTimeout ||
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
	OutcomeListed     Outcome = "listed"
	OutcomeRecovered  Outcome = "recovered"
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

// OperatorAction — закрытая авторизуемая операция над durable evidence.
type OperatorAction string

const (
	OperatorActionRead            OperatorAction = "read"
	OperatorActionDeliveryOutcome OperatorAction = "delivery_outcome"
	OperatorActionRecover         OperatorAction = "recover"
	OperatorActionRepair          OperatorAction = "repair"
)

// OperatorTarget содержит только exact consumer/event coordinates, не authority.
type OperatorTarget struct {
	Action             OperatorAction
	Consumer           Consumer
	IdempotencyKey     string
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
}

// OperatorAuthority возвращается trusted boundary из context/authoritative state.
type OperatorAuthority struct {
	Actor        string
	Organization string
	Project      string
	Operation    string
	KeyHash      [sha256.Size]byte
}

// OperatorAuthorizer назначает actor и canonical durable idempotency scope.
type OperatorAuthorizer interface {
	AuthorizeOperator(context.Context, OperatorTarget) (OperatorAuthority, error)
}

// DeliveryOutcomeRequest задаёт exact immutable identity авторитетного read.
type DeliveryOutcomeRequest struct {
	Consumer    Consumer
	EventID     string
	EventDigest [sha256.Size]byte
}

func (request DeliveryOutcomeRequest) validate() error {
	if err := request.Consumer.validate(); err != nil {
		return err
	}
	if !canonicalUUID(request.EventID) ||
		request.EventDigest == ([sha256.Size]byte{}) {
		return ErrInvalidDeliveryOutcomeRead
	}
	return nil
}

// DeliveryState — закрытое сохранённое состояние exact delivery evidence.
type DeliveryState string

const (
	DeliveryStateReceived   DeliveryState = "RECEIVED"
	DeliveryStateProcessing DeliveryState = "PROCESSING"
	DeliveryStateRetry      DeliveryState = "RETRY"
	DeliveryStateCompleted  DeliveryState = "COMPLETED"
	DeliveryStateStale      DeliveryState = "STALE"
	DeliveryStateDeadLetter DeliveryState = "DEAD_LETTER"
)

// DeliveryDecision — read-only решение для adapter без payload и claim coordinates.
type DeliveryDecision struct {
	State     DeliveryState
	Directive RecoveryDirective
	Action    BrokerAction
	Durable   bool
}

// RepairRequest задаёт bounded audited REQUEUE exact dead-letter predecessor.
type RepairRequest struct {
	Consumer           Consumer
	IdempotencyKey     string // Только вход authorizer; в durable scope не сохраняется.
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

// RecoveryDirective задаёт явный следующий шаг operator/broker adapter.
type RecoveryDirective string

const (
	RecoveryReplayRequired  RecoveryDirective = "replay_required"
	RecoveryWaitPredecessor RecoveryDirective = "wait_predecessor"
	RecoveryWaitLease       RecoveryDirective = "wait_lease"
	RecoveryWaitBackoff     RecoveryDirective = "wait_backoff"
	RecoveryRepairRequired  RecoveryDirective = "repair_required"
	RecoveryACKEligible     RecoveryDirective = "ack_eligible"
)

// RecoveryRequest фиксирует исчерпание внешней redelivery exact event.
type RecoveryRequest struct {
	Consumer           Consumer
	IdempotencyKey     string // Только вход authorizer; в durable scope не сохраняется.
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
	Reason             string
	EvidenceDigest     [sha256.Size]byte
}

func (request RecoveryRequest) validate() error {
	if err := request.Consumer.validate(); err != nil {
		return err
	}
	if len(request.IdempotencyKey) < minimumIdempotencyKeyLength ||
		len(request.IdempotencyKey) > maximumIdempotencyKeyLength ||
		!repairKeyPattern.MatchString(request.IdempotencyKey) ||
		len(request.Reason) == 0 || len(request.Reason) > maximumReasonLength ||
		strings.TrimSpace(request.Reason) != request.Reason ||
		!canonicalUUID(request.EventID) ||
		request.EventDigest == ([sha256.Size]byte{}) ||
		request.EvidenceDigest == ([sha256.Size]byte{}) ||
		request.ExpectedGeneration > math.MaxInt64 || request.ExpectedFence > math.MaxInt64 ||
		(request.ExpectedGeneration == 0) != (request.ExpectedFence == 0) {
		return ErrInvalidRecovery
	}
	return nil
}

// RecoveryReceipt — durable решение без ACK/skip и без payload.
type RecoveryReceipt struct {
	RecoveryID      string
	EventID         string
	EventDigest     [sha256.Size]byte
	Generation      uint64
	Fence           uint64
	Directive       RecoveryDirective
	CreatedAt       time.Time
	AlreadyRecorded bool
}

// BlockageEligibility — закрытая причина блокировки/следующий безопасный шаг.
type BlockageEligibility string

const (
	BlockageReplayRequired  BlockageEligibility = "replay_required"
	BlockageWaitPredecessor BlockageEligibility = "wait_predecessor"
	BlockageWaitLease       BlockageEligibility = "wait_lease"
	BlockageWaitBackoff     BlockageEligibility = "wait_backoff"
	BlockageRepairRequired  BlockageEligibility = "repair_required"
)

// BlockageCursor задаёт bounded keyset pagination без payload/ordering key.
type BlockageCursor struct {
	ReceivedAt time.Time
	EventID    string
}

// BlockageListRequest ограничивает страницу авторитетного operator read.
type BlockageListRequest struct {
	Limit int
	After *BlockageCursor
}

func (request BlockageListRequest) validate() (BlockageListRequest, error) {
	if request.Limit == 0 {
		request.Limit = defaultBlockagePage
	}
	if request.Limit < 1 || request.Limit > maximumBlockagePage {
		return BlockageListRequest{}, ErrInvalidBlockageRead
	}
	if request.After != nil && (request.After.ReceivedAt.IsZero() ||
		!canonicalUUID(request.After.EventID)) {
		return BlockageListRequest{}, ErrInvalidBlockageRead
	}
	return request, nil
}

// BlockageState — закрытое сохранённое состояние blocking predecessor.
type BlockageState string

const (
	BlockageStateReceived   BlockageState = "RECEIVED"
	BlockageStateProcessing BlockageState = "PROCESSING"
	BlockageStateRetry      BlockageState = "RETRY"
	BlockageStateDeadLetter BlockageState = "DEAD_LETTER"
)

// Blockage — безопасные durable coordinates самого раннего predecessor.
type Blockage struct {
	EventID           string
	EventDigest       [sha256.Size]byte
	OrderingKeyDigest [sha256.Size]byte
	EventSequence     uint64
	CursorSequence    uint64
	State             BlockageState
	Eligibility       BlockageEligibility
	Attempts          uint32
	MaxAttempts       uint32
	RepairCount       uint32
	MaxRepairs        uint32
	LeaseGeneration   uint64
	LeaseFence        uint64
	AvailableAt       time.Time
	LeaseExpiresAt    *time.Time
	TerminalAt        *time.Time
	FailureCode       string
	ReceivedAt        time.Time
}

// BlockagePage содержит bounded страницу и keyset continuation.
type BlockagePage struct {
	Items []Blockage
	Next  *BlockageCursor
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
