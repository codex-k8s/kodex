package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// IssueCommand содержит минимальный ввод выпуска контекста.
type IssueCommand struct {
	OperationID  string
	ProofCompact string
}

// IssueResult содержит compact JWS и проверенные claims.
type IssueResult struct {
	Compact string
	Claims  model.AuthorizationClaims
}

// VerifyCommand содержит наблюдаемую целевую границу проверки.
type VerifyCommand struct {
	Compact            string
	ObservedFullMethod string
	DownstreamSPIFFEID string
}

// Authority управляет активным доменным снимком и допуском запросов.
type Authority struct {
	domain         atomic.Pointer[service.Authority]
	available      atomic.Bool
	restoreBlocked atomic.Bool
	attestor       repository.SnapshotAttestor
	activationMu   sync.Mutex
	pendingReceipt repository.SnapshotAttestationReceipt
	pendingState   repository.SnapshotState
	now            func() time.Time
	admissionMu    sync.RWMutex
	inflight       atomic.Int64
}

// NewAuthority создаёт приложение с обязательным независимым attestor.
func NewAuthority(
	domain *service.Authority,
	attestor repository.SnapshotAttestor,
) (*Authority, error) {
	if domain == nil || attestor == nil {
		return nil, errors.New("authority activation boundary is invalid")
	}
	application := &Authority{attestor: attestor, now: time.Now}
	application.domain.Store(domain)
	// Admission закрыт с момента создания. Его открывает только синхронно
	// проверенное внешнее restore-состояние после активации served snapshot.
	application.restoreBlocked.Store(true)
	return application, nil
}

// Issue выпускает контекст через текущий доступный доменный снимок.
func (application *Authority) Issue(
	ctx context.Context,
	command IssueCommand,
) (IssueResult, error) {
	domain, done, err := application.begin()
	if err != nil {
		return IssueResult{}, err
	}
	defer done()
	compact, claims, err := domain.Issue(
		ctx,
		command.OperationID,
		command.ProofCompact,
	)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Compact: compact, Claims: claims}, nil
}

// Verify проверяет контекст через текущий доступный доменный снимок.
func (application *Authority) Verify(
	ctx context.Context,
	command VerifyCommand,
) (model.AuthorizationClaims, error) {
	domain, done, err := application.begin()
	if err != nil {
		return model.AuthorizationClaims{}, err
	}
	defer done()
	return domain.Verify(
		ctx,
		command.Compact,
		command.ObservedFullMethod,
		command.DownstreamSPIFFEID,
	)
}

// ActivateSnapshot подтверждает и открывает исходный снимок.
func (application *Authority) ActivateSnapshot(ctx context.Context) error {
	domain := application.domain.Load()
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	if err := application.activateDomain(ctx, domain, "obtain independent snapshot readback receipt"); err != nil {
		return err
	}
	application.SetAvailable(true)
	return nil
}

// ReplaceActivatedSnapshot атомарно заменяет уже подтверждённый снимок.
func (application *Authority) ReplaceActivatedSnapshot(domain *service.Authority) error {
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	application.domain.Store(domain)
	application.SetAvailable(true)
	return nil
}

// ActivateReplacement подтверждает новый снимок до атомарной замены.
func (application *Authority) ActivateReplacement(
	ctx context.Context,
	domain *service.Authority,
) error {
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	if err := application.activateDomain(ctx, domain, "obtain independent replacement snapshot readback receipt"); err != nil {
		return err
	}
	return application.ReplaceActivatedSnapshot(domain)
}

func (application *Authority) activateDomain(
	ctx context.Context,
	domain *service.Authority,
	attestationError string,
) error {
	application.activationMu.Lock()
	defer application.activationMu.Unlock()

	state := domain.SnapshotState()
	receipt := application.pendingReceipt
	if !sameSnapshotState(application.pendingState, state) ||
		receipt.ReceiptID == "" ||
		!receipt.ExpiresAt.After(application.now().Add(5*time.Second)) {
		var err error
		receipt, err = application.attestor.Attest(ctx, state)
		if err != nil {
			return failure.Wrap(failure.SnapshotRejected, attestationError, err)
		}
		if receipt.ReceiptID == "" ||
			!receipt.ExpiresAt.After(application.now().Add(5*time.Second)) {
			return failure.New(failure.SnapshotRejected, "snapshot readback receipt is expired")
		}
		application.pendingReceipt = receipt
		application.pendingState = cloneSnapshotState(state)
	}
	if err := domain.ActivateSnapshot(ctx, receipt.ReceiptID); err != nil {
		return err
	}
	application.pendingReceipt = repository.SnapshotAttestationReceipt{}
	application.pendingState = repository.SnapshotState{}
	return nil
}

func sameSnapshotState(left, right repository.SnapshotState) bool {
	if left.SourceRevision != right.SourceRevision ||
		left.SourceDigestSHA256 != right.SourceDigestSHA256 ||
		left.PredecessorRevision != right.PredecessorRevision ||
		left.PredecessorDigestSHA256 != right.PredecessorDigestSHA256 ||
		left.KeySetRevision != right.KeySetRevision ||
		left.PolicyRevision != right.PolicyRevision ||
		left.SignerGeneration != right.SignerGeneration ||
		len(left.History) != len(right.History) {
		return false
	}
	for index := range left.History {
		if left.History[index] != right.History[index] {
			return false
		}
	}
	return true
}

func cloneSnapshotState(state repository.SnapshotState) repository.SnapshotState {
	state.AttestationReceiptID = ""
	state.History = append([]repository.RevisionDigest(nil), state.History...)
	return state
}

// SetAvailable изменяет доступность с учётом restore fence.
func (application *Authority) SetAvailable(available bool) {
	application.admissionMu.Lock()
	defer application.admissionMu.Unlock()
	if available && application.restoreBlocked.Load() {
		return
	}
	application.available.Store(available)
}

// SetRestoreBlocked закрывает либо открывает допуск восстановления.
func (application *Authority) SetRestoreBlocked(blocked bool) {
	application.admissionMu.Lock()
	defer application.admissionMu.Unlock()
	application.restoreBlocked.Store(blocked)
	if blocked {
		application.available.Store(false)
	}
}

// WaitDrained ожидает завершения уже допущенных запросов.
func (application *Authority) WaitDrained(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if application.inflight.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("authority inflight drain deadline exceeded")
		case <-ticker.C:
		}
	}
}

// Inflight возвращает число уже допущенных незавершённых запросов.
func (application *Authority) Inflight() int64 {
	return application.inflight.Load()
}

// Ready проверяет полный рабочий путь текущего снимка.
func (application *Authority) Ready(ctx context.Context) error {
	domain, err := application.current()
	if err != nil {
		return err
	}
	return domain.Ready(ctx)
}

// ServedStateReady проверяет обслуживаемое состояние без открытия допуска.
func (application *Authority) ServedStateReady(ctx context.Context) error {
	domain := application.domain.Load()
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	return domain.Ready(ctx)
}

// SnapshotState возвращает копию метаданных текущего снимка.
func (application *Authority) SnapshotState() model.SnapshotState {
	domain := application.domain.Load()
	if domain == nil {
		return model.SnapshotState{}
	}
	state := domain.SnapshotState()
	history := make([]model.RevisionDigest, 0, len(state.History))
	for _, entry := range state.History {
		history = append(history, model.RevisionDigest{
			Revision:     entry.Revision,
			DigestSHA256: entry.DigestSHA256,
		})
	}
	return model.SnapshotState{
		SourceRevision:          state.SourceRevision,
		SourceDigestSHA256:      state.SourceDigestSHA256,
		PredecessorRevision:     state.PredecessorRevision,
		PredecessorDigestSHA256: state.PredecessorDigestSHA256,
		KeySetRevision:          state.KeySetRevision,
		PolicyRevision:          state.PolicyRevision,
		SignerGeneration:        state.SignerGeneration,
		History:                 history,
	}
}

func (application *Authority) current() (*service.Authority, error) {
	if !application.available.Load() {
		return nil, failure.New(
			failure.PersistenceUnavailable,
			"authority snapshot is not ready",
		)
	}
	domain := application.domain.Load()
	if domain == nil {
		return nil, failure.New(
			failure.SnapshotRejected,
			"authority snapshot is unavailable",
		)
	}
	return domain, nil
}

func (application *Authority) begin() (
	*service.Authority,
	func(),
	error,
) {
	application.admissionMu.RLock()
	defer application.admissionMu.RUnlock()
	domain, err := application.current()
	if err != nil {
		return nil, func() {}, err
	}
	application.inflight.Add(1)
	return domain, func() {
		application.inflight.Add(-1)
	}, nil
}
