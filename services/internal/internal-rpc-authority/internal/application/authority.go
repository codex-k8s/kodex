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

// IssueCommand carries the requested operation, compact proof and request digest; the domain still
// verifies the proof and its authority before issuance.
// IssueCommand содержит запрошенную операцию, компактное доказательство и хеш запроса; до выпуска домен
// проверяет доказательство и его полномочия.
type IssueCommand struct {
	OperationID   string
	ProofCompact  string
	RequestDigest string
}

// ContinuationCommand supplies a parent token and exact child-request coordinates for domain
// verification; carrying these fields does not establish accepted parent lineage.
// ContinuationCommand передаёт токен родителя и точные координаты дочернего запроса для доменной
// проверки; сами поля не доказывают принятое происхождение от родителя.
type ContinuationCommand struct {
	OperationID, ParentCompact, RequestID, CorrelationID, RequestDigest string
}

// IssueResult returns the issued compact JWS together with its claims; the signed token is sensitive and
// must not be treated as loggable metadata.
// IssueResult возвращает выпущенный компактный JWS с его данными; подписанный токен конфиденциален и не
// является безопасными метаданными для журналирования.
type IssueResult struct {
	Compact string
	Claims  model.AuthorizationClaims
}

// VerifyCommand combines the compact context with transport-observed RPC, downstream identity and
// request digest; the domain checks their binding rather than trusting payload identity.
// VerifyCommand объединяет компактный контекст с наблюдаемыми транспортом RPC, личностью целевой стороны
// и хешем запроса; домен проверяет их связь, не доверяя личности из содержимого.
type VerifyCommand struct {
	Compact               string
	ObservedFullMethod    string
	DownstreamSPIFFEID    string
	ObservedRequestDigest string
}

// Authority owns the active domain pointer, serialized activation receipts and admission/drain
// bookkeeping. Process-local pending receipts support retry but do not replace durable verification
// state.
// Authority владеет указателем активного домена, сериализованными квитанциями активации и учётом допуска
// и завершения запросов. Локальные ожидающие квитанции поддерживают повтор, но не заменяют устойчивое
// состояние проверки.
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

// NewAuthority requires a domain and independent attestor, stores the initial domain and starts with
// admission restore-blocked and unavailable; construction performs no attestation or activation.
// NewAuthority требует домен и независимую проверку, сохраняет исходный домен и оставляет допуск
// заблокированным восстановлением и недоступным; создание не выполняет аттестацию или активацию.
func NewAuthority(
	domain *service.Authority,
	attestor repository.SnapshotAttestor,
) (*Authority, error) {
	if domain == nil || attestor == nil {
		return nil, errors.New("authority activation boundary is invalid")
	}
	application := &Authority{attestor: attestor, now: time.Now}
	application.domain.Store(domain)
	// Admission starts closed; opening requires synchronously checked external
	// restore state and an activated served snapshot.
	// Допуск изначально закрыт; открытие требует синхронно проверенного внешнего
	// состояния восстановления и активированного обслуживаемого снимка.
	application.restoreBlocked.Store(true)
	return application, nil
}

// Issue acquires one admitted domain reference, delegates proof/request verification and issuance, and
// decrements inflight on every return; it does not hold the admission lock across domain work.
// Issue получает одну допущенную ссылку на домен, передаёт ему проверку доказательства и запроса и
// выпуск, уменьшая счётчик при любом возврате; блокировка допуска не удерживается на время доменной
// работы.
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
		command.RequestDigest,
	)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Compact: compact, Claims: claims}, nil
}

// IssueContinuation brackets the domain's parent/child verification and context issuance with one
// inflight admission; parent lineage and persistent replay rules remain domain-owned.
// IssueContinuation учитывает доменную проверку родителя и потомка и выпуск контекста как один
// допущенный запрос; происхождение и устойчивые правила защиты от повторов принадлежат домену.
func (application *Authority) IssueContinuation(ctx context.Context, command ContinuationCommand) (IssueResult, error) {
	domain, done, err := application.begin()
	if err != nil {
		return IssueResult{}, err
	}
	defer done()
	compact, claims, err := domain.IssueContinuation(ctx, command.OperationID, command.ParentCompact, command.RequestID, command.CorrelationID, command.RequestDigest)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Compact: compact, Claims: claims}, nil
}

// Verify admits one inflight operation and delegates exact token, method, downstream and request-digest
// verification to its captured domain, releasing the counter even on error.
// Verify допускает одну учитываемую операцию и передаёт захваченному домену проверку токена, метода,
// целевой стороны и хеша запроса, освобождая счётчик и при ошибке.
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
		command.ObservedRequestDigest,
	)
}

// ActivateSnapshot independently attests and activates the current domain, then requests availability; a
// still-active restore block prevents admission from opening.
// ActivateSnapshot независимо подтверждает и активирует текущий домен, затем запрашивает доступность;
// действующая блокировка восстановления не позволяет открыть допуск.
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

// ReplaceActivatedSnapshot swaps a nonnil domain pointer and requests availability without independently
// attesting it; the caller must provide an already activated domain and the restore block still applies.
// ReplaceActivatedSnapshot заменяет непустой указатель домена и запрашивает доступность без
// самостоятельной аттестации; вызывающий передаёт уже активированный домен, а блокировка восстановления
// продолжает действовать.
func (application *Authority) ReplaceActivatedSnapshot(domain *service.Authority) error {
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	application.domain.Store(domain)
	application.SetAvailable(true)
	return nil
}

// ActivateReplacement obtains and persists independent activation for the proposed domain before
// swapping the active pointer; failure leaves the previous pointer selected.
// ActivateReplacement получает и сохраняет независимое подтверждение предложенного домена до замены
// активного указателя; при ошибке выбранным остаётся прежний указатель.
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

// activateDomain serializes activation attempts, reuses a pending receipt only for identical snapshot
// coordinates with more than five seconds left, and clears it only after durable activation succeeds.
// Failed persistence retains the receipt for retry.
// activateDomain сериализует попытки активации, использует ожидающую квитанцию лишь для одинаковых
// координат снимка с запасом более пяти секунд и очищает её только после успешной устойчивой активации.
// Ошибка сохранения оставляет квитанцию для повтора.
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

// sameSnapshotState compares source, predecessor, key/policy/signer pins and ordered history for receipt
// reuse; attestation receipt identity itself is deliberately excluded.
// sameSnapshotState сравнивает привязки источника, предшественника, ключей, политики, подписанта и
// упорядоченную историю для повторного использования квитанции; её собственный идентификатор намеренно
// исключён.
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

// cloneSnapshotState clears the storage receipt ID and copies the history slice so cached activation
// coordinates do not share mutable history with the caller.
// cloneSnapshotState очищает идентификатор квитанции хранилища и копирует срез истории, чтобы
// сохранённые координаты активации не разделяли изменяемую историю с вызывающим.
func cloneSnapshotState(state repository.SnapshotState) repository.SnapshotState {
	state.AttestationReceiptID = ""
	state.History = append([]repository.RevisionDigest(nil), state.History...)
	return state
}

// SetAvailable serializes admission changes, always permits closing and ignores an opening request while
// restore is blocked; it does not attest the snapshot or wait for existing requests.
// SetAvailable сериализует изменение допуска, всегда разрешает закрытие и игнорирует открытие при
// блокировке восстановления; аттестация снимка и ожидание уже принятых запросов здесь не выполняются.
func (application *Authority) SetAvailable(available bool) {
	application.admissionMu.Lock()
	defer application.admissionMu.Unlock()
	if available && application.restoreBlocked.Load() {
		return
	}
	application.available.Store(available)
}

// SetRestoreBlocked changes the restore flag under the admission write lock and closes availability when
// blocking; clearing the flag does not reopen availability automatically.
// SetRestoreBlocked меняет флаг восстановления под блокировкой записи допуска и закрывает доступность
// при блокировании; снятие флага не открывает доступность автоматически.
func (application *Authority) SetRestoreBlocked(blocked bool) {
	application.admissionMu.Lock()
	defer application.admissionMu.Unlock()
	application.restoreBlocked.Store(blocked)
	if blocked {
		application.available.Store(false)
	}
}

// WaitDrained polls inflight every ten milliseconds until zero or context cancellation with a fixed
// diagnostic. The caller must close admission first; this method does not prevent new requests and
// returns immediately if already drained.
// WaitDrained проверяет счётчик каждые десять миллисекунд до нуля либо отмены контекста с фиксированной
// диагностикой. Вызывающий сначала закрывает допуск; метод не препятствует новым запросам и сразу
// завершается при нулевом счётчике.
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

// Inflight returns the atomic count of admitted operations that have not invoked their completion
// callback; it is an observation, not a drain barrier.
// Inflight возвращает атомарный счётчик допущенных операций, ещё не вызвавших завершение; это
// наблюдение, а не барьер завершения.
func (application *Authority) Inflight() int64 {
	return application.inflight.Load()
}

// Ready requires current admission availability before delegating full domain readiness; it does not
// increment inflight or open admission itself.
// Ready требует текущей доступности допуска до полной доменной проверки готовности; счётчик операций не
// увеличивается и допуск не открывается.
func (application *Authority) Ready(ctx context.Context) error {
	domain, err := application.current()
	if err != nil {
		return err
	}
	return domain.Ready(ctx)
}

// ServedStateReady checks the loaded domain even while admission is closed, returning a snapshot error
// if absent; it never changes admission flags.
// ServedStateReady проверяет загруженный домен даже при закрытом допуске и возвращает ошибку снимка при
// его отсутствии; флаги допуска не меняются.
func (application *Authority) ServedStateReady(ctx context.Context) error {
	domain := application.domain.Load()
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	return domain.Ready(ctx)
}

// SnapshotState projects a copy of active domain metadata and history without storage receipt identity;
// an absent domain returns zero metadata rather than a fabricated snapshot.
// SnapshotState возвращает копию метаданных и истории активного домена без идентификатора квитанции
// хранилища; отсутствие домена даёт нулевые метаданные, а не выдуманный снимок.
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

// current rejects unavailable admission before loading the active domain and returns distinct fixed
// failures for unavailable service and an absent snapshot; callers own any admission synchronization.
// current отклоняет недоступный допуск до загрузки активного домена и различает фиксированные ошибки
// недоступности и отсутствия снимка; синхронизация допуска принадлежит вызывающему.
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

// begin holds the admission read lock through availability lookup and inflight increment, then returns a
// captured domain and a completion callback that must run exactly once; the callback is not
// independently once-guarded.
// begin удерживает блокировку чтения допуска при проверке доступности и увеличении счётчика, затем
// возвращает захваченный домен и завершение, которое нужно вызвать ровно один раз; собственной защиты от
// повторного вызова у него нет.
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
