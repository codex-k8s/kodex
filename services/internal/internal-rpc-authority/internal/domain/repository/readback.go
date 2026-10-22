package repository

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// ErrNotFound distinguishes absence within the authoritative readback boundary from an unavailable store
// or a malformed signed credential.
// ErrNotFound отличает отсутствие внутри авторитетной readback границы от недоступного store и
// повреждённого signed credential.
var ErrNotFound = errors.New("authority state not found")

// ErrIdempotencyConflict rejects reuse of an idempotency key for a different semantic challenge/receipt
// request rather than replaying an unrelated result.
// ErrIdempotencyConflict отклоняет повтор idempotency key для другого semantic challenge/receipt request
// без повторной выдачи несвязанного результата.
var ErrIdempotencyConflict = errors.New("authority idempotency conflict")

// ErrExpired reports elapsed durable challenge/intent/receipt validity; it does not imply a new effect
// or extended lifetime is authorized.
// ErrExpired сообщает об истёкшем durable challenge/intent/receipt сроке, не разрешая новый эффект и
// продление lifetime.
var ErrExpired = errors.New("authority state expired")

// IssueReadbackChallengeCommand carries server-derived challenge identity, nonce/digest and times
// together with exact peer/intent/credential and semantic idempotency binding for persistence.
// IssueReadbackChallengeCommand несёт server-derived challenge identity, nonce/digest и сроки вместе с
// exact peer/intent/credential и semantic idempotency привязкой для persistence.
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

// ConsumeReadbackChallengeCommand binds one exact challenge and peer to accepted evidence, receipt
// identity, verifier generation and semantic idempotency; storage owns atomic consumption and result
// persistence.
// ConsumeReadbackChallengeCommand связывает exact challenge и peer с принятым evidence, receipt
// identity, verifier generation и semantic idempotency; atomic consumption и сохранение результата
// принадлежат storage.
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

// ReadbackStore owns peer-scoped intents/challenges, one-time consumption receipts and durable served
// trust activation/readiness, keeping raw credential verification in the domain boundary.
// ReadbackStore владеет peer-scoped intents/challenges, одноразовыми consumption receipts и durable
// served trust activation/readiness; проверка сырых credentials остаётся доменной.
type ReadbackStore interface {
	// ResolveReadbackIntent resolves an intent ID inside the verified peer SPIFFE boundary; an identifier
	// alone cannot authorize reading another workload's intent.
	// ResolveReadbackIntent разрешает intent ID в границе проверенного peer SPIFFE; одного идентификатора
	// недостаточно для чтения intent другого workload.
	ResolveReadbackIntent(
		context.Context,
		string,
		string,
	) (model.ReadbackIntent, error)
	// IssueReadbackChallenge persists or rejoins the server-built challenge under exact semantic idempotency
	// and returns its authoritative nonce, identity and validity.
	// IssueReadbackChallenge сохраняет либо повторно получает server-built challenge по exact semantic
	// idempotency и возвращает авторитетные nonce, identity и срок.
	IssueReadbackChallenge(
		context.Context,
		IssueReadbackChallengeCommand,
	) (model.ReadbackChallenge, error)
	// LoadReadbackChallenge reads an exact challenge ID for the verified peer SPIFFE instead of providing an
	// unscoped challenge lookup.
	// LoadReadbackChallenge читает exact challenge ID для проверенного peer SPIFFE вместо scope-free поиска
	// challenge.
	LoadReadbackChallenge(
		context.Context,
		string,
		string,
	) (model.ReadbackChallenge, error)
	// ConsumeReadbackChallenge atomically accepts one challenge/evidence relationship and its immutable
	// receipt so a retry cannot consume the nonce for a different result.
	// ConsumeReadbackChallenge атомарно принимает одну challenge/evidence связь и immutable receipt, не
	// позволяя retry потребить nonce для другого результата.
	ConsumeReadbackChallenge(
		context.Context,
		ConsumeReadbackChallengeCommand,
	) (model.ReadbackReceipt, error)
	// ActivateReadbackTrust advances the exact independently verified served trust state with durable
	// monotonic/predecessor checks before it can satisfy readback readiness.
	// ActivateReadbackTrust продвигает exact независимо проверенный served trust state с durable
	// monotonic/predecessor проверками до использования в readback readiness.
	ActivateReadbackTrust(context.Context, model.ReadbackTrustState) error
	// ReadbackReady checks the caller's expected exact trust snapshot against authoritative served
	// persistence rather than reporting generic database connectivity alone.
	// ReadbackReady сверяет ожидаемый exact trust snapshot с авторитетной served persistence, не сообщая
	// лишь общую доступность database.
	ReadbackReady(context.Context, model.ReadbackTrustState) error
}
