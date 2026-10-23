package repository

import (
	"context"
	"errors"
	"time"
)

// ErrReplay reports a rejected reuse of a durable one-time reservation; callers must not retry it as
// fresh authority.
// ErrReplay сообщает об отклонённом повторе устойчивого одноразового резервирования; повтор не является
// новым полномочием.
var ErrReplay = errors.New("replay reservation rejected")

// ErrSnapshotRollback reports a stale revision or mutated snapshot against the verifier-owned
// high-watermark.
// ErrSnapshotRollback сообщает об устаревшей ревизии либо изменённом снимке относительно границы версий
// проверяющей стороны.
var ErrSnapshotRollback = errors.New("snapshot rollback or mutation rejected")

// ErrNotReady reports that the served snapshot has not satisfied the required persistent readiness
// conditions.
// ErrNotReady сообщает, что обслуживаемый снимок ещё не удовлетворяет обязательным устойчивым условиям
// готовности.
var ErrNotReady = errors.New("served snapshot is not ready")

// ErrParentNotAccepted rejects a continuation whose parent lacks the required accepted verification
// record.
// ErrParentNotAccepted отклоняет продолжение, для родителя которого отсутствует требуемая запись
// принятой проверки.
var ErrParentNotAccepted = errors.New("continuation parent is not accepted")

// ReservationKind distinguishes durable proof consumption from authorization-context consumption; the
// scopes are not interchangeable.
// ReservationKind различает устойчивое потребление доказательства и контекста авторизации; их области не
// взаимозаменяемы.
type ReservationKind string

// Reservation kinds keep proof and authorization-context replay scopes distinct.
// Виды резервирования разделяют области защиты от повторов доказательства и контекста авторизации.
const (
	// ReservationAuthorityProof selects the replay namespace for consuming a caller authority proof.
	// ReservationAuthorityProof выбирает пространство защиты от повторов при потреблении доказательства
	// полномочий вызывающей стороны.
	ReservationAuthorityProof ReservationKind = "AUTHORITY_PROOF"
	// ReservationAuthorizationContext selects the replay namespace for consuming a signed internal-RPC
	// authorization context.
	// ReservationAuthorizationContext выбирает пространство защиты от повторов при потреблении подписанного
	// контекста авторизации внутреннего RPC.
	ReservationAuthorizationContext ReservationKind = "AUTHORIZATION_CONTEXT"
)

// Reservation binds one-time consumption to caller, signer generation, operation, revision, JTI, digest
// and expiry; the kind also participates in its replay scope.
// Reservation связывает одноразовое потребление с вызывающей стороной, поколением подписанта, операцией,
// ревизией, JTI, хешем и сроком; вид также входит в область защиты от повторов.
type Reservation struct {
	CallerWorkloadID string
	SignerGeneration uint64
	Kind             ReservationKind
	ScopeID          string
	OperationID      string
	Issuer           string
	Revision         uint64
	JTI              string
	Digest           string
	ExpiresAt        time.Time
}

// IssuedContextBinding records the issued token digest, caller and target with issuance and expiry
// times, plus an optional parent binding; it is a receipt input, not caller authority.
// IssuedContextBinding фиксирует хеш выпущенного токена, вызывающую и целевую стороны, время выпуска и
// окончания, а также необязательную связь с родителем; это данные квитанции, не полномочие клиента.
type IssuedContextBinding struct {
	JTI, Digest, CallerWorkloadID, TargetWorkloadID string
	IssuedAt, ExpiresAt                             time.Time
	ParentJTI, ParentDigest                         string
}

// SnapshotState pins the served snapshot, predecessor history, key/policy revisions and signer
// generation together with the independent attestation receipt used by persistence checks.
// SnapshotState закрепляет обслуживаемый снимок, историю предшественников, ревизии ключей и политик и
// поколение подписанта вместе с независимой квитанцией аттестации для устойчивых проверок.
type SnapshotState struct {
	SourceRevision          uint64
	SourceDigestSHA256      string
	PredecessorRevision     uint64
	PredecessorDigestSHA256 string
	KeySetRevision          uint64
	PolicyRevision          uint64
	SignerGeneration        uint64
	History                 []RevisionDigest
	AttestationReceiptID    string
}

// SnapshotAttestationReceipt carries the independent receipt identity and server-issued expiry; the
// receiving client must not replace it with a locally calculated lifetime.
// SnapshotAttestationReceipt несёт идентификатор независимой квитанции и назначенный сервером срок;
// принимающий клиент не подменяет его локально вычисленным временем жизни.
type SnapshotAttestationReceipt struct {
	ReceiptID string
	ExpiresAt time.Time
}

// SnapshotFreshness exposes the persisted receipt identity and validity with PostgreSQL-owned
// observation time, rather than trusting a caller clock or TTL.
// SnapshotFreshness возвращает устойчивый идентификатор и срок квитанции со временем наблюдения
// PostgreSQL, а не доверяет часам или TTL вызывающей стороны.
type SnapshotFreshness struct {
	ReceiptID  string
	ValidUntil time.Time
	ObservedAt time.Time
}

// RevisionDigest pairs a historical source revision with its canonical SHA-256 digest for persistent
// rollback and mutation checks.
// RevisionDigest связывает историческую ревизию источника с каноническим SHA-256 для устойчивой проверки
// откатов и изменений.
type RevisionDigest struct {
	Revision     uint64
	DigestSHA256 string
}

// Store defines durable replay consumption, issued-context lineage and snapshot high-watermarks;
// implementations own the transactional verification boundary.
// Store задаёт устойчивое потребление одноразовых полномочий, происхождение выпущенных контекстов и
// границу версий снимка; реализация владеет транзакционной границей проверки.
type Store interface {
	// Reserve persists a one-time reservation or rejects replay; callers supply an already validated binding
	// rather than an arbitrary reusable token.
	// Reserve сохраняет одноразовое резервирование либо отклоняет повтор; вызывающая сторона передаёт уже
	// проверенную привязку, а не произвольный многократный токен.
	Reserve(ctx context.Context, reservation Reservation) error
	// RegisterIssuedContext records a newly issued context against the pinned snapshot, including its
	// caller/target and parent lineage when present.
	// RegisterIssuedContext фиксирует выпущенный контекст относительно закреплённого снимка, включая
	// вызывающую и целевую стороны и происхождение от родителя при его наличии.
	RegisterIssuedContext(ctx context.Context, state SnapshotState, binding IssuedContextBinding) error
	// ReserveContinuation consumes the child reservation only through the required accepted parent binding;
	// matching caller-supplied identifiers alone do not establish lineage.
	// ReserveContinuation потребляет резервирование потомка только через требуемую принятую связь с
	// родителем; совпадения переданных клиентом идентификаторов недостаточно для происхождения.
	ReserveContinuation(ctx context.Context, parent Reservation, child Reservation) error
	// ActivateSnapshot advances or validates the persistent served-snapshot high-watermark and rejects
	// rollback or mutation instead of silently replacing history.
	// ActivateSnapshot продвигает либо проверяет устойчивую границу обслуживаемого снимка и отклоняет откат
	// или изменение вместо молчаливой замены истории.
	ActivateSnapshot(ctx context.Context, state SnapshotState) error
	// Freshness reads persisted attestation freshness for the exact snapshot and returns the storage-owned
	// observation time used for validity decisions.
	// Freshness читает устойчивую актуальность аттестации точного снимка и возвращает время наблюдения
	// хранилища для решения о сроке действия.
	Freshness(ctx context.Context, state SnapshotState) (SnapshotFreshness, error)
	// AcceptVerification accepts the reservation against the supplied snapshot within the store's
	// verification boundary, rather than separating snapshot eligibility from replay consumption.
	// AcceptVerification принимает резервирование относительно указанного снимка внутри границы проверки
	// хранилища, не разрывая допустимость снимка и потребление одноразового полномочия.
	AcceptVerification(
		ctx context.Context,
		state SnapshotState,
		reservation Reservation,
	) error
	// Ready checks that persistent verification state can serve the expected snapshot; successful dependency
	// connectivity alone does not satisfy this contract.
	// Ready проверяет возможность обслуживать ожидаемый снимок по устойчивому состоянию проверки; одной
	// доступности зависимости для этого контракта недостаточно.
	Ready(ctx context.Context, expected SnapshotState) error
	// Close releases store resources after its users have stopped; it does not represent a snapshot
	// transition or revoke an issued context.
	// Close освобождает ресурсы хранилища после остановки его потребителей; это не переход снимка и не отзыв
	// выпущенного контекста.
	Close()
}

// SnapshotAttestor obtains independent evidence for the exact served snapshot instead of allowing a
// verifier to manufacture its own freshness receipt.
// SnapshotAttestor получает независимое свидетельство для точного обслуживаемого снимка вместо
// самостоятельного создания проверяющей стороной квитанции актуальности.
type SnapshotAttestor interface {
	// Attest requests an independent receipt for the supplied snapshot and returns its server-issued
	// lifetime; failure must not become local success.
	// Attest запрашивает независимую квитанцию указанного снимка с назначенным сервером сроком; ошибка не
	// превращается в локальное подтверждение.
	Attest(context.Context, SnapshotState) (SnapshotAttestationReceipt, error)
}
