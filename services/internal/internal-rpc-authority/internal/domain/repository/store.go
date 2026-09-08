package repository

import (
	"context"
	"errors"
	"time"
)

// ErrReplay сообщает о повторном использовании одноразового идентификатора.
var ErrReplay = errors.New("replay reservation rejected")

// ErrSnapshotRollback сообщает об откате либо мутации снимка.
var ErrSnapshotRollback = errors.New("snapshot rollback or mutation rejected")

// ErrNotReady сообщает, что обслуживаемый снимок ещё не подтверждён.
var ErrNotReady = errors.New("served snapshot is not ready")

// ErrParentNotAccepted сообщает, что continuation не связан с ранее принятым parent.
var ErrParentNotAccepted = errors.New("continuation parent is not accepted")

// ReservationKind различает одноразовые proof и authorization context.
type ReservationKind string

// Поддерживаемые назначения устойчивого резервирования.
const (
	// ReservationAuthorityProof и следующее значение образуют закрытый набор.
	ReservationAuthorityProof       ReservationKind = "AUTHORITY_PROOF"
	ReservationAuthorizationContext ReservationKind = "AUTHORIZATION_CONTEXT"
)

// Reservation задаёт устойчивую одноразовую запись replay protection.
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

// IssuedContextBinding связывает JWS с серверным receipt; deadline не задаётся caller.
type IssuedContextBinding struct {
	JTI, Digest, CallerWorkloadID, TargetWorkloadID string
	IssuedAt, ExpiresAt                             time.Time
	ParentJTI, ParentDigest                         string
}

// SnapshotState задаёт проверяемый обслуживаемый снимок и его историю.
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

// SnapshotAttestationReceipt связывает независимый receipt с его серверным
// сроком действия. Клиент не вычисляет expiry локально.
type SnapshotAttestationReceipt struct {
	ReceiptID string
	ExpiresAt time.Time
}

// SnapshotFreshness содержит только verifier-owned время и identity receipt.
// ObservedAt назначает PostgreSQL; клиент не переносит сюда свой clock/TTL.
type SnapshotFreshness struct {
	ReceiptID  string
	ValidUntil time.Time
	ObservedAt time.Time
}

// RevisionDigest связывает revision с каноническим SHA-256 digest.
type RevisionDigest struct {
	Revision     uint64
	DigestSHA256 string
}

// Store владеет replay reservations и persistent snapshot high-watermark.
type Store interface {
	Reserve(ctx context.Context, reservation Reservation) error
	RegisterIssuedContext(ctx context.Context, state SnapshotState, binding IssuedContextBinding) error
	ReserveContinuation(ctx context.Context, parent Reservation, child Reservation) error
	ActivateSnapshot(ctx context.Context, state SnapshotState) error
	Freshness(ctx context.Context, state SnapshotState) (SnapshotFreshness, error)
	AcceptVerification(
		ctx context.Context,
		state SnapshotState,
		reservation Reservation,
	) error
	Ready(ctx context.Context, expected SnapshotState) error
	Close()
}

// SnapshotAttestor получает независимый receipt обслуживаемого снимка.
type SnapshotAttestor interface {
	Attest(context.Context, SnapshotState) (SnapshotAttestationReceipt, error)
}
