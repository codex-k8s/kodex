package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestRestoreBlockAtomicallyClosesAdmissionAndWaitsForInflight checks that blocking closes new
// admission, generic readiness cannot reopen it and a local drain waits for the captured operation
// before explicit reopening.
// TestRestoreBlockAtomicallyClosesAdmissionAndWaitsForInflight проверяет закрытие нового допуска при
// блокировке, невозможность открыть его общей готовностью и ожидание принятой операции до явного
// повторного открытия.
func TestRestoreBlockAtomicallyClosesAdmissionAndWaitsForInflight(t *testing.T) {
	t.Parallel()
	authority := &Authority{}
	authority.domain.Store(&service.Authority{})
	authority.SetAvailable(true)

	_, done, err := authority.begin()
	if err != nil {
		t.Fatalf("begin admitted operation: %v", err)
	}
	authority.SetRestoreBlocked(true)
	if authority.available.Load() {
		t.Fatal("restore block did not close admission")
	}
	if _, _, err := authority.begin(); err == nil {
		t.Fatal("operation was admitted after restore block")
	}
	authority.SetAvailable(true)
	if authority.available.Load() {
		t.Fatal("generic readiness reopened a restore-blocked authority")
	}

	drainDone := make(chan error, 1)
	go func() {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drainDone <- authority.WaitDrained(drainCtx)
	}()
	select {
	case err := <-drainDone:
		t.Fatalf("drain completed before inflight operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	done()
	if err := <-drainDone; err != nil {
		t.Fatalf("wait for inflight drain: %v", err)
	}

	authority.SetRestoreBlocked(false)
	authority.SetAvailable(true)
	if !authority.available.Load() {
		t.Fatal("authority did not reopen after restore fence release")
	}
}

// TestActivateSnapshotReusesPendingReceiptUntilWatermarkPersists injects one durable activation failure
// and requires receipt reuse on retry, then fresh attestation after successful activation clears the
// cache.
// TestActivateSnapshotReusesPendingReceiptUntilWatermarkPersists вводит один отказ устойчивой активации
// и требует повторного использования квитанции, затем новой аттестации после очистки кэша успешной
// активацией.
func TestActivateSnapshotReusesPendingReceiptUntilWatermarkPersists(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 26, 1, 0, 0, 0, time.UTC)
	store := &activationStore{activationErrors: []error{
		repository.ErrSnapshotRollback,
		nil,
		nil,
	}}
	domain := newActivationAuthority(t, now, store)
	attestor := &activationAttestor{now: now}
	application, err := NewAuthority(domain, attestor)
	if err != nil {
		t.Fatalf("create authority application: %v", err)
	}
	application.now = func() time.Time { return now }

	if err := application.ActivateSnapshot(context.Background()); err == nil {
		t.Fatal("incomplete publisher promotion was accepted")
	}
	if err := application.ActivateSnapshot(context.Background()); err != nil {
		t.Fatalf("retry pending snapshot activation: %v", err)
	}
	if attestor.calls != 1 {
		t.Fatalf("pending receipt was reissued: calls=%d", attestor.calls)
	}
	if len(store.receiptIDs) != 2 || store.receiptIDs[0] != store.receiptIDs[1] {
		t.Fatalf("pending receipt was not reused: ids=%v", store.receiptIDs)
	}

	if err := application.ActivateSnapshot(context.Background()); err != nil {
		t.Fatalf("refresh activated snapshot: %v", err)
	}
	if attestor.calls != 2 || store.receiptIDs[2] == store.receiptIDs[1] {
		t.Fatalf("successful activation did not clear pending receipt: calls=%d ids=%v", attestor.calls, store.receiptIDs)
	}
}

// activationAttestor is a deterministic local attestation fixture that counts calls and changes its
// synthetic receipt after the first issuance; it performs no remote verification.
// activationAttestor — воспроизводимая локальная оснастка аттестации со счётчиком вызовов и сменой
// синтетической квитанции после первого выпуска; удалённой проверки здесь нет.
type activationAttestor struct {
	now   time.Time
	calls int
}

// Attest returns a five-minute fixture receipt relative to the configured clock and records the call;
// supplied snapshot contents are intentionally ignored by this limited fake.
// Attest возвращает квитанцию оснастки на пять минут относительно заданных часов и учитывает вызов;
// содержимое снимка ограниченная оснастка намеренно не проверяет.
func (attestor *activationAttestor) Attest(
	context.Context,
	repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	attestor.calls++
	receiptID := "82d62007-3a12-4c43-8a0b-1dbf694f55b4"
	if attestor.calls > 1 {
		receiptID = "76932eaf-7d68-4a4d-a8dc-22d4ffc7aa72"
	}
	return repository.SnapshotAttestationReceipt{
		ReceiptID: receiptID,
		ExpiresAt: attestor.now.Add(5 * time.Minute),
	}, nil
}

// activationStore captures attempted receipt IDs and returns a configured sequence of activation errors;
// unrelated port methods are no-op scaffolding, not storage or authorization proof.
// activationStore сохраняет идентификаторы попыток активации и возвращает заданную последовательность
// ошибок; остальные методы — заглушки, а не доказательство хранения или авторизации.
type activationStore struct {
	activationErrors []error
	receiptIDs       []string
}

// Reserve is an unused successful replay-port stub for activation tests and does not record or enforce
// one-time reservations.
// Reserve — неиспользуемая успешная заглушка порта защиты от повторов для тестов активации;
// резервирование не сохраняется и не проверяется.
func (store *activationStore) Reserve(context.Context, repository.Reservation) error {
	return nil
}

// ReserveContinuation is a successful activation-fixture stub with no parent-lineage or
// child-consumption checks; those contracts are tested elsewhere.
// ReserveContinuation — успешная заглушка оснастки активации без проверки происхождения от родителя и
// потребления потомка; эти контракты проверяются отдельно.
func (store *activationStore) ReserveContinuation(
	context.Context,
	repository.Reservation,
	repository.Reservation,
) error {
	return nil
}

// ActivateSnapshot records the supplied receipt ID and returns the error at that attempt index, allowing
// tests to observe retry identity without a PostgreSQL transaction.
// ActivateSnapshot фиксирует переданный идентификатор квитанции и возвращает ошибку соответствующей
// попытки, позволяя проверить идентичность повтора без транзакции PostgreSQL.
func (store *activationStore) ActivateSnapshot(
	_ context.Context,
	state repository.SnapshotState,
) error {
	store.receiptIDs = append(store.receiptIDs, state.AttestationReceiptID)
	index := len(store.receiptIDs) - 1
	if index < len(store.activationErrors) {
		return store.activationErrors[index]
	}
	return nil
}

// AcceptVerification is an unused successful verification stub required by the fixture port and does not
// prove snapshot eligibility or replay consumption.
// AcceptVerification — неиспользуемая успешная заглушка обязательного метода порта; допустимость снимка
// и одноразовое потребление она не доказывает.
func (store *activationStore) AcceptVerification(
	context.Context,
	repository.SnapshotState,
	repository.Reservation,
) error {
	return nil
}

// Ready always succeeds for the activation fixture and deliberately supplies no independent dependency
// or persisted-snapshot readiness evidence.
// Ready всегда успешен в оснастке активации и намеренно не даёт независимого свидетельства готовности
// зависимости или сохранённого снимка.
func (store *activationStore) Ready(context.Context, repository.SnapshotState) error {
	return nil
}

// Close is a no-op because the fixture owns no pool, connection or background worker.
// Close ничего не делает, поскольку оснастка не владеет пулом, соединением или фоновым обработчиком.
func (store *activationStore) Close() {}

// newActivationAuthority builds a synthetic domain with a fresh ES256 public verification key, fixed
// policy pins and the supplied store; no real workload credential or transport is used.
// newActivationAuthority создаёт синтетический домен с новым открытым ключом проверки ES256,
// фиксированными привязками политики и переданным хранилищем; настоящих учётных данных нагрузки и
// транспорта здесь нет.
func newActivationAuthority(
	t *testing.T,
	now time.Time,
	store repository.Store,
) *service.Authority {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key("activation-g1")
	if err != nil {
		t.Fatalf("generate activation key: %v", err)
	}
	// issuer pins the synthetic policy's signing identity, not a live credential.
	// issuer закрепляет личность подписанта синтетической политики, не живые учётные данные.
	const issuer = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	domain, err := service.NewAuthority(model.PolicySnapshot{
		Version: 1, AuthorityABIVersion: model.AuthorityABIVersion,
		DefaultDecision: "DENY", TokenTTLSeconds: 30,
		AllowedClockSkewSeconds: 2, SourceRevision: 1,
		SourceDigestSHA256:      strings.Repeat("a", 64),
		PredecessorDigestSHA256: strings.Repeat("0", 64),
		KeySetRevision:          1, PolicyRevision: 1, SignerGeneration: 1,
		Issuer: issuer, SignerKeyID: key.KeyID,
	}, service.KeyMaterial{
		VerificationKeys: map[string]service.VerificationKeyRecord{
			key.KeyID: {
				Key: key.PublicOnly(), Issuer: issuer, Generation: 1,
				Status: "CURRENT", Purpose: "AUTHORIZATION_CONTEXT",
				Audiences: map[string]struct{}{"urn:kodex:test": {}},
				NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("create activation authority: %v", err)
	}
	return domain
}

// Freshness returns synthetic storage metadata from the wall clock and the supplied receipt ID with a
// thirty-second window; it is not evidence of a database-owned clock.
// Freshness возвращает синтетические метаданные по системным часам и переданному идентификатору
// квитанции с окном в тридцать секунд; это не доказательство времени владельца базы.
func (*activationStore) Freshness(_ context.Context, state repository.SnapshotState) (repository.SnapshotFreshness, error) {
	now := time.Now()
	return repository.SnapshotFreshness{ReceiptID: state.AttestationReceiptID, ObservedAt: now, ValidUntil: now.Add(30 * time.Second)}, nil
}

// RegisterIssuedContext is an unused successful stub for the activation fixture; no issued-token lineage
// is persisted.
// RegisterIssuedContext — неиспользуемая успешная заглушка оснастки активации; происхождение выпущенного
// токена не сохраняется.
func (*activationStore) RegisterIssuedContext(context.Context, repository.SnapshotState, repository.IssuedContextBinding) error {
	return nil
}
