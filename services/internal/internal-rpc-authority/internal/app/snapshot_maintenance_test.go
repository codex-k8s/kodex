package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// snapshotMaintenanceStub independently controls served-readiness and activation errors and counts both calls for deterministic local maintenance tests. It simulates a runtime port without a database, real attestor, timer or signed receipt.
// snapshotMaintenanceStub независимо задаёт ошибки обслуживаемой готовности и активации и считает оба вызова для детерминированных локальных тестов обслуживания. Моделируется порт runtime без БД, реального attestor, таймера или подписанной квитанции.
type snapshotMaintenanceStub struct {
	readyErr      error
	activationErr error
	readyCalls    int
	activations   int
}

// ServedStateReady increments only the readiness counter and returns the configured error, allowing tests to detect skipped or repeated probes without observing real receipt state.
// ServedStateReady увеличивает только счётчик готовности и возвращает настроенную ошибку, позволяя обнаружить пропущенные или повторные проверки без чтения реального состояния квитанции.
func (stub *snapshotMaintenanceStub) ServedStateReady(context.Context) error {
	stub.readyCalls++
	return stub.readyErr
}

// ActivateSnapshot increments only the activation counter and returns the configured error; it never updates readiness state, advances time or creates a receipt implicitly.
// ActivateSnapshot увеличивает только счётчик активации и возвращает настроенную ошибку; неявного обновления готовности, продвижения времени или создания квитанции нет.
func (stub *snapshotMaintenanceStub) ActivateSnapshot(context.Context) error {
	stub.activations++
	return stub.activationErr
}

// TestMaintainServedSnapshotUsesExistingReadbackBeforeRefresh uses a synthetic half-interval time and successful readiness stub to require one probe, no activation and the unchanged refresh timestamp.
// TestMaintainServedSnapshotUsesExistingReadbackBeforeRefresh использует синтетическое время половины интервала и успешную заглушку готовности, требуя одну проверку, отсутствие активации и неизменное время обновления.
func TestMaintainServedSnapshotUsesExistingReadbackBeforeRefresh(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-snapshotReadbackRefreshInterval / 2)
	stub := &snapshotMaintenanceStub{}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, lastRefresh, now)
	if err != nil {
		t.Fatalf("maintain served snapshot: %v", err)
	}
	if refreshedAt != lastRefresh || stub.readyCalls != 1 || stub.activations != 0 {
		t.Fatalf("unexpected maintenance result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotRefreshesBeforeReceiptExpiry places local time exactly at the refresh interval and requires one successful activation, no preliminary readiness probe and the new timestamp. It does not measure a real receipt's expiry despite the scenario name.
// TestMaintainServedSnapshotRefreshesBeforeReceiptExpiry ставит локальное время точно на интервал обновления и требует одну успешную активацию, отсутствие предварительной проверки готовности и новую временную метку. Несмотря на название сценария, реальный срок квитанции не измеряется.
func TestMaintainServedSnapshotRefreshesBeforeReceiptExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{}

	refreshedAt, err := maintainServedSnapshot(
		context.Background(),
		stub,
		now.Add(-snapshotReadbackRefreshInterval),
		now,
	)
	if err != nil {
		t.Fatalf("refresh served snapshot: %v", err)
	}
	if refreshedAt != now || stub.readyCalls != 0 || stub.activations != 1 {
		t.Fatalf("unexpected refresh result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotKeepsValidReceiptAfterTransientRefreshFailure injects gRPC Unavailable at a due refresh and successful served readiness, requiring one activation, one probe and the old timestamp. Eligibility comes from the stub response, not an actual attestation or deadline extension.
// TestMaintainServedSnapshotKeepsValidReceiptAfterTransientRefreshFailure подставляет gRPC Unavailable при наступившем обновлении и успешную обслуживаемую готовность, требуя одну активацию, одну проверку и старую метку времени. Допустимость задаётся ответом заглушки, а не реальной аттестацией или продлением срока.
func TestMaintainServedSnapshotKeepsValidReceiptAfterTransientRefreshFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-snapshotReadbackRefreshInterval)
	stub := &snapshotMaintenanceStub{
		activationErr: status.Error(codes.Unavailable, "readback unavailable"),
	}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, lastRefresh, now)
	if err != nil {
		t.Fatalf("maintain served snapshot: %v", err)
	}
	if refreshedAt != lastRefresh || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected deferred refresh result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotRejectsNonTransportRefreshFailureImmediately injects PermissionDenied and requires an error after one activation without any readiness fallback, retaining the old local timestamp.
// TestMaintainServedSnapshotRejectsNonTransportRefreshFailureImmediately подставляет PermissionDenied и требует ошибку после одной активации без запасной проверки готовности с сохранением прежней локальной метки времени.
func TestMaintainServedSnapshotRejectsNonTransportRefreshFailureImmediately(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-snapshotReadbackRefreshInterval)
	stub := &snapshotMaintenanceStub{
		activationErr: status.Error(codes.PermissionDenied, "readback rejected"),
	}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, lastRefresh, now)
	if err == nil {
		t.Fatal("non-transport readback failure used last-known-good state")
	}
	if refreshedAt != lastRefresh || stub.readyCalls != 0 || stub.activations != 1 {
		t.Fatalf("unexpected fail-closed result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotRejectsExpiredReceiptAfterRefreshFailure combines an unavailable activation with a readiness-expired stub and requires failure, one call of each kind and no timestamp advancement; it does not wait for an actual receipt to expire.
// TestMaintainServedSnapshotRejectsExpiredReceiptAfterRefreshFailure совмещает недоступную активацию с заглушкой истёкшей готовности и требует отказ, по одному вызову каждого вида и отсутствие продвижения времени; реальное истечение квитанции не ожидается.
func TestMaintainServedSnapshotRejectsExpiredReceiptAfterRefreshFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-snapshotReadbackRefreshInterval)
	stub := &snapshotMaintenanceStub{
		readyErr:      errors.New("receipt expired"),
		activationErr: status.Error(codes.Unavailable, "readback unavailable"),
	}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, lastRefresh, now)
	if err == nil {
		t.Fatal("expired receipt was accepted after failed refresh")
	}
	if refreshedAt != lastRefresh || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected rejected refresh result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotRecoversRejectedReadbackImmediately makes the early readiness probe fail within the local refresh interval and requires immediate successful activation with the supplied current timestamp rather than waiting for another scheduled refresh.
// TestMaintainServedSnapshotRecoversRejectedReadbackImmediately задаёт отказ ранней проверки готовности внутри локального интервала и требует немедленную успешную активацию с переданной текущей меткой времени вместо ожидания следующего обновления.
func TestMaintainServedSnapshotRecoversRejectedReadbackImmediately(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{readyErr: errors.New("receipt expired")}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, now, now.Add(time.Second))
	if err != nil {
		t.Fatalf("recover served snapshot: %v", err)
	}
	if refreshedAt != now.Add(time.Second) || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected recovery result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestMaintainServedSnapshotKeepsFailureClosed rejects the combination of early readiness failure and unavailable activation, checking one probe/activation and the unchanged timestamp instead of accepting last-known-good state blindly.
// TestMaintainServedSnapshotKeepsFailureClosed отклоняет сочетание раннего отказа готовности и недоступной активации, проверяя один вызов каждого вида и неизменное время вместо слепого принятия прежнего состояния.
func TestMaintainServedSnapshotKeepsFailureClosed(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{
		readyErr:      errors.New("receipt expired"),
		activationErr: status.Error(codes.Unavailable, "readback unavailable"),
	}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, now, now.Add(time.Second))
	if err == nil {
		t.Fatal("unavailable readback was accepted")
	}
	if refreshedAt != now || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected failed recovery result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

// TestReplacementFailurePreservesOnlyAuthoritativeBoundedReceipt checks five synthetic replacement outcomes: transient success fallback, expired/database-unavailable refusal and denial/corruption without a readiness read. It requires zero activations throughout, proving local selection logic rather than real signed or durable receipt validity.
// TestReplacementFailurePreservesOnlyAuthoritativeBoundedReceipt проверяет пять синтетических исходов замены: допустимый запасной путь при временном отказе, отказ при истечении или недоступной БД и отказ при запрете или повреждении без чтения готовности. Во всех случаях требуется ноль активаций; доказывается локальный выбор, а не реальная допустимость подписанной или устойчивой квитанции.
func TestReplacementFailurePreservesOnlyAuthoritativeBoundedReceipt(t *testing.T) {
	for _, test := range []struct {
		name         string
		cause, ready error
		retained     bool
		reads        int
	}{
		{"transient", status.Error(codes.Unavailable, "attestor unavailable"), nil, true, 1},
		{"expired", status.Error(codes.Unavailable, "attestor unavailable"), errors.New("receipt expired"), false, 1},
		{"database unavailable", status.Error(codes.Unavailable, "attestor unavailable"), errors.New("database unavailable"), false, 1},
		{"denied", status.Error(codes.PermissionDenied, "readback denied"), nil, false, 0},
		{"corruption", errors.New("snapshot signature rejected"), nil, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &snapshotMaintenanceStub{readyErr: test.ready}
			if retained := retainServedSnapshotAfterReplacementFailure(t.Context(), stub, test.cause); retained != test.retained || stub.readyCalls != test.reads || stub.activations != 0 {
				t.Fatal("replacement failure changed fixed served receipt policy")
			}
		})
	}
}
