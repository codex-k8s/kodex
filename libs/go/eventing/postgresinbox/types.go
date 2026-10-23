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

	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/jackc/pgx/v5"
)

const (
	// maximumConsumerLength caps both durable consumer name and scope identifiers.
	// maximumConsumerLength ограничивает имена и области устойчивых потребителей.
	maximumConsumerLength = 128
	// maximumSchemaLength follows the PostgreSQL identifier bound for an inbox schema.
	// maximumSchemaLength соответствует пределу идентификатора схемы inbox в PostgreSQL.
	maximumSchemaLength = 63
	// maximumInstanceLength bounds the lease owner's runtime identity.
	// maximumInstanceLength ограничивает runtime-идентичность владельца аренды.
	maximumInstanceLength = 128
	// maximumErrorCodeLength bounds the safe persisted effect failure category.
	// maximumErrorCodeLength ограничивает безопасную сохраняемую категорию ошибки эффекта.
	maximumErrorCodeLength = 63
	// maximumIdempotencyKeyLength caps untrusted operator request keys before authorization.
	// maximumIdempotencyKeyLength ограничивает недоверенный ключ оператора до авторизации.
	maximumIdempotencyKeyLength = 128
	// maximumActorLength bounds the trusted actor stored with an operator receipt.
	// maximumActorLength ограничивает доверенного actor в квитанции операции.
	maximumActorLength = 256
	// maximumReasonLength caps audited operator reasoning without admitting unbounded text.
	// maximumReasonLength ограничивает аудируемое обоснование оператора.
	maximumReasonLength = 1024
	// minimumIdempotencyKeyLength rejects trivially short operator request keys.
	// minimumIdempotencyKeyLength отклоняет слишком короткие ключи запроса оператора.
	minimumIdempotencyKeyLength = 8
	// minimumRetentionHorizon keeps completed evidence at least one day.
	// minimumRetentionHorizon сохраняет завершённые свидетельства минимум сутки.
	minimumRetentionHorizon = 24 * time.Hour
	// maximumRetentionHorizon caps durable event retention to ten years.
	// maximumRetentionHorizon ограничивает хранение устойчивых событий десятью годами.
	maximumRetentionHorizon = 10 * 365 * 24 * time.Hour
	// maximumLeaseDuration prevents a worker from holding an event indefinitely.
	// maximumLeaseDuration не позволяет исполнителю удерживать событие бесконечно.
	maximumLeaseDuration = 15 * time.Minute
	// maximumBackoff caps delay before retrying a failed event.
	// maximumBackoff ограничивает задержку перед повтором неуспешного события.
	maximumBackoff = 24 * time.Hour
	// maximumCleanupBatch bounds rows removed per retention transaction.
	// maximumCleanupBatch ограничивает число строк в одной транзакции очистки.
	maximumCleanupBatch = 1000
	// maximumTransactionRetries caps retries after serialization or unique conflicts.
	// maximumTransactionRetries ограничивает повторы после конфликтов сериализации или уникальности.
	maximumTransactionRetries = 3
	// maximumBlockagePage caps one authorized operator inspection page.
	// maximumBlockagePage ограничивает одну страницу авторизованного просмотра блокировок.
	maximumBlockagePage = 100
	// defaultBlockagePage applies when an operator omits the page limit.
	// defaultBlockagePage применяется, когда оператор не указал размер страницы.
	defaultBlockagePage = 50
	// schemaVersion pins the installed inbox schema contract generation.
	// schemaVersion закрепляет поколение установленного контракта схемы inbox.
	schemaVersion = 1
	// schemaComponent identifies this inbox's independent migration marker.
	// schemaComponent обозначает отдельный маркер миграции этого inbox.
	schemaComponent = "postgresinbox"
	// schemaDigestHex pins exact migration content for readiness readback.
	// schemaDigestHex закрепляет точное содержимое миграции для проверки готовности.
	schemaDigestHex = "4c44aeb7b45033cd140b9d49db24d67d0ff620687249879d3274427e1e29d5f2"
)

var (
	// consumerNamePattern restricts the stable consumer name grammar.
	// consumerNamePattern ограничивает грамматику устойчивого имени потребителя.
	consumerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	// consumerScopePattern restricts the durable consumer scope without whitespace.
	// consumerScopePattern ограничивает устойчивую область потребителя без пробелов.
	consumerScopePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	// schemaNamePattern excludes quoted or dynamically escaped database identifiers.
	// schemaNamePattern исключает заключённые в кавычки или динамически экранируемые имена схем.
	schemaNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	// instanceIDPattern restricts the lease owner to a bounded transport-safe identity.
	// instanceIDPattern ограничивает владельца аренды безопасной для передачи идентичностью.
	instanceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
	// errorCodePattern permits only bounded persisted failure-code identifiers.
	// errorCodePattern допускает только ограниченные идентификаторы сохраняемых кодов ошибок.
	errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	// repairKeyPattern requires a bounded operator idempotency key of at least eight bytes.
	// repairKeyPattern требует ограниченный ключ идемпотентности оператора не короче восьми байт.
	repairKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,127}$`)
	// effectNamePattern restricts effect registry names and function identifiers.
	// effectNamePattern ограничивает имена реестра эффектов и идентификаторы функций.
	effectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// Beginner opens PostgreSQL transactions owned and bounded by the processor.
// Beginner открывает PostgreSQL-транзакции, принадлежащие процессору и ограниченные им.
type Beginner interface {
	// BeginTx starts a transaction using the processor's exact options.
	// BeginTx начинает транзакцию с точными параметрами процессора.
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// EventSnapshot holds a pinned envelope copy without shared mutable payload memory.
// EventSnapshot хранит закреплённую копию конверта без общей изменяемой памяти содержимого.
type EventSnapshot struct {
	envelope eventing.Envelope
}

// Envelope returns a detached copy so handlers cannot mutate the pinned event.
// Envelope возвращает отдельную копию, чтобы обработчик не менял закреплённое событие.
func (snapshot EventSnapshot) Envelope() eventing.Envelope {
	return cloneEnvelope(snapshot.envelope)
}

// Data returns detached JSON bytes for effect handlers without sharing the inbox snapshot.
// Data возвращает отдельные байты JSON обработчику эффекта без общей памяти со снимком inbox.
func (snapshot EventSnapshot) Data() json.RawMessage {
	return append(json.RawMessage(nil), snapshot.envelope.Data...)
}

// EffectOperation is an opaque reference to a registered service-owned SQL effect function.
// EffectOperation — непрозрачная ссылка на зарегистрированную SQL-функцию эффекта сервиса.
type EffectOperation struct {
	name     string
	schema   string
	function string
	query    string
}

// NewEffectOperation constructs an exact schema-qualified jsonb effect call after identifier checks.
// NewEffectOperation создаёт точный вызов функции эффекта jsonb после проверки идентификаторов.
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

// Name returns the bounded registry key without exposing the SQL function or query.
// Name возвращает ограниченный ключ реестра без раскрытия SQL-функции или запроса.
func (operation EffectOperation) Name() string { return operation.name }

// EffectTx grants only registered calls within an existing transaction, not raw SQL or session control.
// EffectTx разрешает только зарегистрированные вызовы в существующей транзакции, не произвольный SQL или управление сеансом.
type EffectTx interface {
	// Call invokes one registered effect with a bounded JSON input under the current transaction.
	// Call вызывает один зарегистрированный эффект с ограниченным JSON в текущей транзакции.
	Call(EffectOperation, json.RawMessage) (json.RawMessage, error)
}

// Handler performs a local effect through the narrow transaction-bound capability and immutable snapshot.
// Handler выполняет локальный эффект через узкое полномочие транзакции и неизменяемый снимок.
type Handler func(context.Context, EffectTx, EventSnapshot) error

// Consumer identifies a server-assigned durable reader by name and scope.
// Consumer обозначает назначенного сервером устойчивого читателя именем и областью.
type Consumer struct {
	Name  string
	Scope string
}

// validate rejects malformed consumer coordinates before they reach inbox storage.
// validate отклоняет неверные координаты потребителя до обращения к хранилищу inbox.
func (consumer Consumer) validate() error {
	if len(consumer.Name) == 0 || len(consumer.Name) > maximumConsumerLength ||
		!consumerNamePattern.MatchString(consumer.Name) ||
		len(consumer.Scope) == 0 || len(consumer.Scope) > maximumConsumerLength ||
		!consumerScopePattern.MatchString(consumer.Scope) {
		return ErrInvalidConsumer
	}
	return nil
}

// Config bounds processor lease, effect, retry, retention, and cleanup behavior.
// Config ограничивает аренду, эффект, повторы, хранение и очистку процессора.
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

// withDefaults fills only the optional finalize timeout and cleanup batch without changing other limits.
// withDefaults заполняет только необязательные таймаут завершения и размер очистки, сохраняя остальные лимиты.
func (config Config) withDefaults() Config {
	if config.FinalizeTimeout == 0 {
		config.FinalizeTimeout = 5 * time.Second
	}
	if config.CleanupBatchSize == 0 {
		config.CleanupBatchSize = 100
	}
	return config
}

// validate checks bounded schema identity and that effect and finalize budgets fit inside a lease.
// validate проверяет ограниченную идентичность схемы и размещение сроков эффекта и завершения внутри аренды.
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

// BrokerAction is the closed broker-adapter response, valid only with durable evidence.
// BrokerAction — закрытый ответ адаптера брокера, допустимый только при устойчивом свидетельстве.
type BrokerAction string

const (
	// BrokerActionACK acknowledges a durably applied or stale event.
	// BrokerActionACK подтверждает устойчиво обработанное или устаревшее событие.
	BrokerActionACK BrokerAction = "ack"
	// BrokerActionNACKRetry preserves an event for later ordered redelivery.
	// BrokerActionNACKRetry сохраняет событие для последующей доставки по порядку.
	BrokerActionNACKRetry BrokerAction = "nack_retry"
	// BrokerActionNACKTerminal stops automatic redelivery after conflict or dead letter.
	// BrokerActionNACKTerminal останавливает автоматическую доставку после конфликта или dead-letter.
	BrokerActionNACKTerminal BrokerAction = "nack_terminal"
)

// Outcome is a closed low-cardinality result for inbox observation and broker decisions.
// Outcome — закрытый исход inbox низкой кардинальности для наблюдения и решения брокера.
type Outcome string

const (
	// OutcomeProcessed means the handler effect and cursor advance committed together.
	// OutcomeProcessed означает совместную фиксацию эффекта обработчика и продвижения курсора.
	OutcomeProcessed Outcome = "processed"
	// OutcomeClaimed means an exact exclusive lease was acquired for this event.
	// OutcomeClaimed означает получение точной исключительной аренды события.
	OutcomeClaimed Outcome = "claimed"
	// OutcomeRenewed means the same fenced lease was durably extended.
	// OutcomeRenewed означает устойчивое продление той же ограждённой аренды.
	OutcomeRenewed Outcome = "renewed"
	// OutcomeDuplicate means prior completion makes replay safe to acknowledge.
	// OutcomeDuplicate означает, что прежнее завершение позволяет подтвердить повтор.
	OutcomeDuplicate Outcome = "duplicate"
	// OutcomeStale means the ordering cursor has already passed this sequence.
	// OutcomeStale означает, что курсор порядка уже прошёл этот номер.
	OutcomeStale Outcome = "stale"
	// OutcomeGap means a predecessor must be received before this event proceeds.
	// OutcomeGap означает ожидание предшественника перед обработкой события.
	OutcomeGap Outcome = "gap"
	// OutcomeBusy means another live lease currently owns the exact event.
	// OutcomeBusy означает, что точное событие занято другой действующей арендой.
	OutcomeBusy Outcome = "busy"
	// OutcomeRetry means the event is waiting for its bounded retry window.
	// OutcomeRetry означает ожидание ограниченного срока повтора события.
	OutcomeRetry Outcome = "retry"
	// OutcomeDeadLetter means automatic attempts ended without implicit acknowledgement.
	// OutcomeDeadLetter означает завершение автоматических попыток без неявного подтверждения.
	OutcomeDeadLetter Outcome = "dead_letter"
	// OutcomeConflict means the event identity disagrees with durable evidence.
	// OutcomeConflict означает расхождение идентичности события с устойчивыми данными.
	OutcomeConflict Outcome = "conflict"
	// OutcomeRepaired means an authorized bounded dead-letter requeue committed.
	// OutcomeRepaired означает фиксацию разрешённого ограниченного повторного запуска dead-letter.
	OutcomeRepaired Outcome = "repaired"
	// OutcomeCleaned counts safely expired terminal rows removed by retention.
	// OutcomeCleaned учитывает безопасно удалённые по сроку завершённые строки.
	OutcomeCleaned Outcome = "cleaned"
	// OutcomeReady means exact schema and runtime-principal probes succeeded.
	// OutcomeReady означает успешную проверку точной схемы и рабочего пути учётной записи.
	OutcomeReady Outcome = "ready"
	// OutcomeListed means an authorized bounded blockage page was read.
	// OutcomeListed означает чтение авторизованной ограниченной страницы блокировок.
	OutcomeListed Outcome = "listed"
	// OutcomeRecovered means an authorized recovery directive was recorded durably.
	// OutcomeRecovered означает устойчивую запись разрешённой директивы восстановления.
	OutcomeRecovered Outcome = "recovered"
	// OutcomeCanceled means the caller's context stopped an unfinished operation.
	// OutcomeCanceled означает остановку незавершённой операции контекстом вызывающего.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeError means the operation failed without a more specific safe outcome.
	// OutcomeError означает сбой операции без более точного безопасного исхода.
	OutcomeError Outcome = "error"
)

// Result may be converted into a broker action only when Durable proves committed evidence.
// Result можно превратить в действие брокера только при Durable с подтверждённой фиксацией.
type Result struct {
	Outcome Outcome
	Action  BrokerAction
	Durable bool
}

// Claim binds one exact event and digest to a server-issued owner, token, generation, and fence.
// Claim связывает точное событие и дайджест с назначенными сервером владельцем, токеном, поколением и fence.
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

// EffectFailure classifies a handler failure while suppressing its private cause from display.
// EffectFailure классифицирует ошибку обработчика, скрывая закрытую причину при отображении.
type EffectFailure struct {
	code      string
	retryable bool
	cause     error
}

// NewEffectFailure normalizes an unsafe or absent effect code to a bounded default category.
// NewEffectFailure заменяет неверный или отсутствующий код эффекта ограниченной категорией по умолчанию.
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

// Error exposes only the fixed safe effect diagnostic, not the original cause.
// Error выдаёт только фиксированную безопасную диагностику эффекта, но не исходную причину.
func (failure *EffectFailure) Error() string { return errorTextEffectFailed }

// Unwrap preserves programmatic cause matching without exposing it in Error.
// Unwrap сохраняет программное сопоставление причины, не раскрывая её через Error.
func (failure *EffectFailure) Unwrap() error { return failure.cause }

// Code returns the bounded category persisted with the failed inbox event.
// Code возвращает ограниченную категорию, сохраняемую с неуспешным событием inbox.
func (failure *EffectFailure) Code() string { return failure.code }

// Retryable tells the processor whether another bounded delivery attempt is allowed.
// Retryable сообщает процессору, допустима ли следующая ограниченная попытка доставки.
func (failure *EffectFailure) Retryable() bool { return failure.retryable }

// OperatorAction is a closed authority domain for reading or changing durable evidence.
// OperatorAction — закрытая область полномочий для чтения или изменения устойчивых данных.
type OperatorAction string

const (
	// OperatorActionRead authorizes inspection of blockers without state mutation.
	// OperatorActionRead разрешает просмотр блокировок без изменения состояния.
	OperatorActionRead OperatorAction = "read"
	// OperatorActionDeliveryOutcome authorizes exact delivery readback by event digest.
	// OperatorActionDeliveryOutcome разрешает точное чтение результата доставки по дайджесту.
	OperatorActionDeliveryOutcome OperatorAction = "delivery_outcome"
	// OperatorActionRecover authorizes a durable recovery directive for one event.
	// OperatorActionRecover разрешает устойчивую директиву восстановления одного события.
	OperatorActionRecover OperatorAction = "recover"
	// OperatorActionRepair authorizes an audited dead-letter requeue with exact fencing.
	// OperatorActionRepair разрешает аудируемый повторный запуск dead-letter с точным fence.
	OperatorActionRepair OperatorAction = "repair"
)

// OperatorTarget carries exact consumer/event coordinates but never grants authority itself.
// OperatorTarget содержит точные координаты потребителя и события, но не выдаёт полномочий.
type OperatorTarget struct {
	Action             OperatorAction
	Consumer           Consumer
	IdempotencyKey     string
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
}

// OperatorAuthority is assigned by the trusted boundary from verified actor and resource state.
// OperatorAuthority назначается доверенной границей из проверенных actor и состояния ресурса.
type OperatorAuthority struct {
	Actor        string
	Organization string
	Project      string
	Operation    string
	KeyHash      [sha256.Size]byte
}

// OperatorAuthorizer resolves actor and canonical durable idempotency scope for an exact target.
// OperatorAuthorizer определяет actor и каноническую устойчивую область идемпотентности для точной цели.
type OperatorAuthorizer interface {
	// AuthorizeOperator returns server-owned authority or rejects the requested target.
	// AuthorizeOperator возвращает назначенное сервером полномочие либо отклоняет цель.
	AuthorizeOperator(context.Context, OperatorTarget) (OperatorAuthority, error)
}

// DeliveryOutcomeRequest pins event ID and digest for an authoritative delivery read.
// DeliveryOutcomeRequest закрепляет ID и дайджест события для авторитетного чтения доставки.
type DeliveryOutcomeRequest struct {
	Consumer    Consumer
	EventID     string
	EventDigest [sha256.Size]byte
}

// validate rejects absent consumer, noncanonical event ID, or empty digest before authorization.
// validate отклоняет отсутствие потребителя, неканонический ID или пустой дайджест до авторизации.
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

// DeliveryState is the closed persisted state of one exact event delivery.
// DeliveryState — закрытое сохранённое состояние доставки одного точного события.
type DeliveryState string

const (
	// DeliveryStateReceived retains an event not yet held by a worker.
	// DeliveryStateReceived сохраняет событие, ещё не занятое исполнителем.
	DeliveryStateReceived DeliveryState = "RECEIVED"
	// DeliveryStateProcessing records an event under a live exact lease.
	// DeliveryStateProcessing фиксирует событие под действующей точной арендой.
	DeliveryStateProcessing DeliveryState = "PROCESSING"
	// DeliveryStateRetry waits for a safe bounded redelivery.
	// DeliveryStateRetry ожидает безопасной ограниченной повторной доставки.
	DeliveryStateRetry DeliveryState = "RETRY"
	// DeliveryStateCompleted records a committed handler effect and cursor advance.
	// DeliveryStateCompleted фиксирует эффект обработчика и продвижение курсора.
	DeliveryStateCompleted DeliveryState = "COMPLETED"
	// DeliveryStateStale records a sequence already passed by the cursor.
	// DeliveryStateStale фиксирует номер, уже пройденный курсором.
	DeliveryStateStale DeliveryState = "STALE"
	// DeliveryStateDeadLetter retains terminal work for explicit operator repair.
	// DeliveryStateDeadLetter сохраняет завершённую работу для явного исправления оператором.
	DeliveryStateDeadLetter DeliveryState = "DEAD_LETTER"
)

// DeliveryDecision gives the adapter a read-only directive without payload or claim coordinates.
// DeliveryDecision даёт адаптеру директиву только для чтения, без содержимого и координат аренды.
type DeliveryDecision struct {
	State     DeliveryState
	Directive RecoveryDirective
	Action    BrokerAction
	Durable   bool
}

// RepairRequest asks to requeue one exact dead-letter predecessor under audited fencing.
// RepairRequest запрашивает повторный запуск одного точного dead-letter предшественника под аудируемым fence.
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

// validate bounds repair key, reason, evidence, and required generation/fence before authorization.
// validate ограничивает ключ, причину, свидетельство и обязательные generation/fence до авторизации.
func (request RepairRequest) validate() error {
	if err := request.Consumer.validate(); err != nil {
		return err
	}
	if !validOperatorRequestEvidence(request.IdempotencyKey, request.EventID,
		request.EventDigest, request.ExpectedGeneration, request.ExpectedFence,
		request.Reason, request.EvidenceDigest) ||
		request.ExpectedGeneration == 0 || request.ExpectedFence == 0 {
		return ErrInvalidRepair
	}
	return nil
}

// RepairReceipt is immutable durable evidence of a requeue, including replay detection.
// RepairReceipt — неизменные устойчивые данные повторного запуска, включая признак повтора.
type RepairReceipt struct {
	RepairID        string
	EventID         string
	EventDigest     [sha256.Size]byte
	Generation      uint64
	Fence           uint64
	CreatedAt       time.Time
	AlreadyRepaired bool
}

// RecoveryDirective is a closed next step for operator and broker adapters, not an implicit ACK.
// RecoveryDirective — закрытый следующий шаг оператора и адаптера брокера, а не неявный ACK.
type RecoveryDirective string

const (
	// RecoveryReplayRequired requests redelivery when the event remains eligible.
	// RecoveryReplayRequired требует повторной доставки, пока событие допускает её.
	RecoveryReplayRequired RecoveryDirective = "replay_required"
	// RecoveryWaitPredecessor holds the event until its ordering gap closes.
	// RecoveryWaitPredecessor удерживает событие до закрытия разрыва порядка.
	RecoveryWaitPredecessor RecoveryDirective = "wait_predecessor"
	// RecoveryWaitLease defers action while another exact lease remains active.
	// RecoveryWaitLease откладывает действие, пока действует другая точная аренда.
	RecoveryWaitLease RecoveryDirective = "wait_lease"
	// RecoveryWaitBackoff defers redelivery until its available-at deadline.
	// RecoveryWaitBackoff откладывает доставку до времени available-at.
	RecoveryWaitBackoff RecoveryDirective = "wait_backoff"
	// RecoveryRepairRequired requires an explicit bounded operator action.
	// RecoveryRepairRequired требует явного ограниченного действия оператора.
	RecoveryRepairRequired RecoveryDirective = "repair_required"
	// RecoveryACKEligible allows ACK only after completed or stale durable evidence.
	// RecoveryACKEligible разрешает ACK только при устойчивом завершении или устаревании.
	RecoveryACKEligible RecoveryDirective = "ack_eligible"
)

// RecoveryRequest records external redelivery exhaustion against an exact event and evidence digest.
// RecoveryRequest фиксирует исчерпание внешних доставок точного события и дайджеста свидетельства.
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

// validate bounds recovery input and requires generation and fence to be both present or both absent.
// validate ограничивает запрос восстановления и требует совместного наличия либо отсутствия generation и fence.
func (request RecoveryRequest) validate() error {
	if err := request.Consumer.validate(); err != nil {
		return err
	}
	if !validOperatorRequestEvidence(request.IdempotencyKey, request.EventID,
		request.EventDigest, request.ExpectedGeneration, request.ExpectedFence,
		request.Reason, request.EvidenceDigest) ||
		(request.ExpectedGeneration == 0) != (request.ExpectedFence == 0) {
		return ErrInvalidRecovery
	}
	return nil
}

// validOperatorRequestEvidence checks only the shared untrusted coordinates, never action authority or fence policy.
// validOperatorRequestEvidence проверяет только общие недоверенные координаты, но не полномочие действия и правило fence.
func validOperatorRequestEvidence(
	idempotencyKey, eventID string,
	eventDigest [sha256.Size]byte,
	expectedGeneration, expectedFence uint64,
	reason string,
	evidenceDigest [sha256.Size]byte,
) bool {
	return len(idempotencyKey) >= minimumIdempotencyKeyLength &&
		len(idempotencyKey) <= maximumIdempotencyKeyLength &&
		repairKeyPattern.MatchString(idempotencyKey) &&
		len(reason) > 0 && len(reason) <= maximumReasonLength &&
		strings.TrimSpace(reason) == reason &&
		canonicalUUID(eventID) &&
		eventDigest != ([sha256.Size]byte{}) &&
		evidenceDigest != ([sha256.Size]byte{}) &&
		expectedGeneration <= math.MaxInt64 && expectedFence <= math.MaxInt64
}

// RecoveryReceipt is durable recovery evidence without payload or an implicit ACK/skip.
// RecoveryReceipt — устойчивое свидетельство восстановления без содержимого и неявного ACK/skip.
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

// BlockageEligibility is a closed reason and safe next step for the earliest blocker.
// BlockageEligibility — закрытая причина и безопасный следующий шаг для первой блокировки.
type BlockageEligibility string

const (
	// BlockageReplayRequired allows a blocked event to be safely retried.
	// BlockageReplayRequired разрешает безопасный повтор заблокированного события.
	BlockageReplayRequired BlockageEligibility = "replay_required"
	// BlockageWaitPredecessor waits for an earlier ordering sequence.
	// BlockageWaitPredecessor ожидает более ранний номер в порядке событий.
	BlockageWaitPredecessor BlockageEligibility = "wait_predecessor"
	// BlockageWaitLease waits for another worker's live lease to end.
	// BlockageWaitLease ожидает завершения действующей аренды другого исполнителя.
	BlockageWaitLease BlockageEligibility = "wait_lease"
	// BlockageWaitBackoff waits until the event is available for retry.
	// BlockageWaitBackoff ожидает готовности события к повтору.
	BlockageWaitBackoff BlockageEligibility = "wait_backoff"
	// BlockageRepairRequired marks dead-letter work requiring explicit repair.
	// BlockageRepairRequired обозначает dead-letter работу для явного исправления.
	BlockageRepairRequired BlockageEligibility = "repair_required"
)

// BlockageCursor carries only time and event ID for bounded keyset pagination.
// BlockageCursor несёт только время и ID события для ограниченной keyset-пагинации.
type BlockageCursor struct {
	ReceivedAt time.Time
	EventID    string
}

// BlockageListRequest bounds one authoritative operator read page and optional continuation.
// BlockageListRequest ограничивает страницу авторитетного чтения оператором и продолжение.
type BlockageListRequest struct {
	Limit int
	After *BlockageCursor
}

// validate fills the default page size and rejects oversized or malformed cursors.
// validate подставляет размер страницы по умолчанию и отклоняет неверные или слишком большие курсоры.
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

// BlockageState is the closed persisted state of an unfinished predecessor.
// BlockageState — закрытое сохранённое состояние незавершённого предшественника.
type BlockageState string

const (
	// BlockageStateReceived marks a predecessor not yet leased.
	// BlockageStateReceived обозначает предшественника, ещё не занятого арендой.
	BlockageStateReceived BlockageState = "RECEIVED"
	// BlockageStateProcessing marks a predecessor held by a worker lease.
	// BlockageStateProcessing обозначает предшественника под арендой исполнителя.
	BlockageStateProcessing BlockageState = "PROCESSING"
	// BlockageStateRetry marks a predecessor awaiting bounded redelivery.
	// BlockageStateRetry обозначает предшественника, ожидающего ограниченной доставки.
	BlockageStateRetry BlockageState = "RETRY"
	// BlockageStateDeadLetter marks a terminal predecessor until operator repair.
	// BlockageStateDeadLetter обозначает завершённого предшественника до исправления оператором.
	BlockageStateDeadLetter BlockageState = "DEAD_LETTER"
)

// Blockage exposes safe durable coordinates of the earliest predecessor without its payload.
// Blockage выдаёт безопасные устойчивые координаты первого предшественника без содержимого.
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

// BlockagePage holds a bounded blocker page and optional keyset continuation.
// BlockagePage содержит ограниченную страницу блокировок и необязательное keyset-продолжение.
type BlockagePage struct {
	Items []Blockage
	Next  *BlockageCursor
}

// classifyEffectFailure preserves explicit handler classification or defaults to a safe retryable code.
// classifyEffectFailure сохраняет явную классификацию обработчика либо задаёт безопасный код с повтором.
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
