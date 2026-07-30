package repository

import (
	"context"
	"errors"
	"time"
)

var (
	ErrReplay           = errors.New("replay reservation rejected")
	ErrSnapshotRollback = errors.New("snapshot rollback or mutation rejected")
	ErrNotReady         = errors.New("served snapshot is not ready")
)

type ReservationKind string

const (
	ReservationAuthorityProof       ReservationKind = "AUTHORITY_PROOF"
	ReservationAuthorizationContext ReservationKind = "AUTHORIZATION_CONTEXT"
)

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

type RevisionDigest struct {
	Revision     uint64
	DigestSHA256 string
}

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
