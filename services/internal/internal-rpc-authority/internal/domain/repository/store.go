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
	Kind        ReservationKind
	ScopeID     string
	OperationID string
	Issuer      string
	Revision    uint64
	JTI         string
	Digest      string
	ExpiresAt   time.Time
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

// RevisionDigest связывает revision с каноническим SHA-256 digest.
type RevisionDigest struct {
	Revision     uint64
	DigestSHA256 string
}

// Store владеет replay reservations и persistent snapshot high-watermark.
type Store interface {
	Reserve(ctx context.Context, reservation Reservation) error
	ActivateSnapshot(ctx context.Context, state SnapshotState) error
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
	Attest(context.Context, SnapshotState) (string, error)
}
