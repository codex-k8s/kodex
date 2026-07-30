package repository

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

// ErrNotFound скрывает отсутствие состояния в авторитетной границе.
var ErrNotFound = errors.New("authority state not found")

// ErrIdempotencyConflict сообщает о несовпадении семантики повторного запроса.
var ErrIdempotencyConflict = errors.New("authority idempotency conflict")

// ErrExpired сообщает об истечении срока устойчивого состояния.
var ErrExpired = errors.New("authority state expired")

// IssueReadbackChallengeCommand задаёт server-derived выпуск challenge.
type IssueReadbackChallengeCommand struct {
	IntentID                 string
	PeerSPIFFEID             string
	ReadbackCredentialJTI    string
	ReadbackCredentialDigest string
	IdempotencyKey           string
	SemanticRequestDigest    string
	ChallengeID              string
	ChallengeJTI             string
	ChallengeNonce           string
	ChallengeDigestSHA256    string
	IssuedAt                 time.Time
	ExpiresAt                time.Time
}

// ConsumeReadbackChallengeCommand задаёт атомарное потребление challenge.
type ConsumeReadbackChallengeCommand struct {
	ChallengeID           string
	PeerSPIFFEID          string
	ReceiptID             string
	EvidenceJTI           string
	EvidenceDigestSHA256  string
	IdempotencyKey        string
	SemanticRequestDigest string
	VerifierGeneration    uint64
	AcceptedAt            time.Time
	ExpiresAt             time.Time
}

// ReadbackStore владеет challenge, receipt и readiness проверки доставки.
type ReadbackStore interface {
	ResolveReadbackIntent(
		context.Context,
		string,
		string,
	) (model.ReadbackIntent, error)
	IssueReadbackChallenge(
		context.Context,
		IssueReadbackChallengeCommand,
	) (model.ReadbackChallenge, error)
	LoadReadbackChallenge(
		context.Context,
		string,
		string,
	) (model.ReadbackChallenge, error)
	ConsumeReadbackChallenge(
		context.Context,
		ConsumeReadbackChallengeCommand,
	) (model.ReadbackReceipt, error)
	ActivateReadbackTrust(context.Context, model.ReadbackTrustState) error
	ReadbackReady(context.Context, model.ReadbackTrustState) error
}
