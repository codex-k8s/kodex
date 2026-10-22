package restoreagent

import (
	"context"
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The normal-poll unit test injects a safe OPEN response and proves no admission changes occur before it arrives, followed by ready/unblock/available ordering. It uses a recording boundary without real controller RPC or signed transition verification.
// TestPollНеЗакрываетAdmissionВоВремяШтатнойПроверки внедряет безопасный ответ OPEN и проверяет отсутствие изменений допуска до его получения, затем порядок ready, unblock и available. Используется записывающая граница без реального RPC контроллера и проверки подписанного перехода.
func TestPollНеЗакрываетAdmissionВоВремяШтатнойПроверки(t *testing.T) {
	admission := &recordingAdmission{}
	agent := &Agent{
		now: func() time.Time { return time.Unix(100, 0).UTC() },
		directiveFetcher: func(context.Context) (
			*internalrpcauthorityv1.GetRestoreDirectiveResponse,
			string,
			error,
		) {
			if len(admission.calls) != 0 {
				t.Fatalf("admission changed before restore controller response: %v", admission.calls)
			}
			return &internalrpcauthorityv1.GetRestoreDirectiveResponse{
				Result: &internalrpcauthorityv1.GetRestoreDirectiveResponse_NoDirective{
					NoDirective: &internalrpcauthorityv1.NoRestoreDirective{
						CoordinationRevision: 3,
						RestoreEpoch:         2,
						VerifiedTransition: safeTransition(
							internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN,
						),
					},
				},
			}, "", nil
		},
	}
	if err := agent.Poll(context.Background(), admission); err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	expected := []string{"served-state-ready", "restore-blocked:false", "available:true"}
	if strings.Join(admission.calls, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected admission transitions: got %v, want %v", admission.calls, expected)
	}
}

// The fail-closed unit test rejects a missing transition and QUIESCING/PREPARED/RESTORING fixture phases using only the local predicate; no real startup or external poll runs.
// TestVerifySafeNoDirectiveFailClosedДоПервогоВнешнегоPoll отклоняет отсутствие перехода и эталонные фазы QUIESCING, PREPARED, RESTORING только локальным предикатом; реального запуска и внешнего poll нет.
func TestVerifySafeNoDirectiveFailClosedДоПервогоВнешнегоPoll(t *testing.T) {
	agent := &Agent{now: func() time.Time { return time.Unix(100, 0).UTC() }}
	result := &internalrpcauthorityv1.NoRestoreDirective{
		CoordinationRevision: 3,
		RestoreEpoch:         2,
	}
	if err := agent.verifySafeNoDirective(result, nil); err == nil {
		t.Fatal("missing external transition opened startup barrier")
	}
	for _, phase := range []internalrpcauthorityv1.RestorePhase{
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_QUIESCING,
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_PREPARED,
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_RESTORING,
	} {
		transition := safeTransition(phase)
		if err := agent.verifySafeNoDirective(result, transition); err == nil {
			t.Fatalf("phase %s opened startup barrier", phase)
		}
	}
}

// The safe-window unit test uses a fixed clock to accept OPEN, reject a future COMPLETED safe window and accept the same window exactly at its boundary. It does not exercise certificate parsing, trust or durable restore state.
// TestVerifySafeNoDirectiveПринимаетТолькоOpenИСозревшийCompleted с фиксированными часами принимает OPEN, отклоняет будущее безопасное окно COMPLETED и принимает его точно на границе. Разбор сертификата, доверие и устойчивое состояние восстановления не проверяются.
func TestVerifySafeNoDirectiveПринимаетТолькоOpenИСозревшийCompleted(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	agent := &Agent{now: func() time.Time { return now }}
	result := &internalrpcauthorityv1.NoRestoreDirective{
		CoordinationRevision: 3,
		RestoreEpoch:         2,
	}
	open := safeTransition(
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN,
	)
	if err := agent.verifySafeNoDirective(result, open); err != nil {
		t.Fatalf("OPEN rejected: %v", err)
	}
	completed := safeTransition(
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_COMPLETED,
	)
	completed.SafeWindowNotBefore = timestamppb.New(now.Add(time.Second))
	if err := agent.verifySafeNoDirective(result, completed); err == nil {
		t.Fatal("pre-recovery COMPLETED opened startup barrier")
	}
	completed.SafeWindowNotBefore = timestamppb.New(now)
	if err := agent.verifySafeNoDirective(result, completed); err != nil {
		t.Fatalf("mature COMPLETED rejected: %v", err)
	}
}

// safeTransition builds a minimal synthetic transition with fixed restore/epoch/anchor/evidence fields and a caller-selected phase for local predicate tests; it is not server-issued proof.
// safeTransition создаёт минимальный синтетический переход с фиксированными полями восстановления, эпохи, опорной ревизии и доказательства и выбранной фазой для локальных тестов предиката; это не серверное доказательство.
func safeTransition(
	phase internalrpcauthorityv1.RestorePhase,
) *internalrpcauthorityv1.RestoreTransition {
	return &internalrpcauthorityv1.RestoreTransition{
		RestoreId:            "00000000-0000-4000-8000-000000000001",
		Phase:                phase,
		AnchorRevision:       2,
		RestoreEpoch:         2,
		EvidenceDigestSha256: strings.Repeat("a", 64),
	}
}

// recordingAdmission records flag/readiness calls for ordering assertions and supplies an already-drained empty snapshot fixture; it does not emulate real concurrent admission or storage.
// recordingAdmission записывает вызовы флагов и готовности для проверки порядка и предоставляет уже дренированный пустой снимок; реальный конкурентный допуск и хранилище не эмулируются.
type recordingAdmission struct {
	calls []string
}

// SetAvailable appends the requested availability flag to the fixture call log without changing a real listener or readiness endpoint.
// SetAvailable добавляет запрошенный флаг доступности в журнал вызовов без изменения реального listener или endpoint готовности.
func (admission *recordingAdmission) SetAvailable(value bool) {
	admission.calls = append(admission.calls, "available:"+boolString(value))
}

// SetRestoreBlocked records the requested restore-fence flag in order without implementing an admission barrier.
// SetRestoreBlocked по порядку записывает запрошенный флаг ограждения восстановления без реализации барьера допуска.
func (admission *recordingAdmission) SetRestoreBlocked(value bool) {
	admission.calls = append(admission.calls, "restore-blocked:"+boolString(value))
}

// WaitDrained succeeds immediately in this fixture without waiting on requests or observing context cancellation.
// WaitDrained в этом эталоне немедленно успешен без ожидания запросов или наблюдения отмены контекста.
func (*recordingAdmission) WaitDrained(context.Context) error { return nil }

// Inflight always returns zero to model an already-drained workload, not concurrent request accounting.
// Inflight всегда возвращает ноль, моделируя уже дренированный workload, а не конкурентный учёт запросов.
func (*recordingAdmission) Inflight() int64 { return 0 }

// SnapshotState returns an empty snapshot fixture and performs no readback or authoritative state lookup.
// SnapshotState возвращает пустой эталон снимка без readback и обращения к авторитетному состоянию.
func (*recordingAdmission) SnapshotState() model.SnapshotState {
	return model.SnapshotState{}
}

// ServedStateReady records its place in the admission call sequence and always succeeds; it does not check a receipt or storage readiness.
// ServedStateReady записывает своё место в последовательности вызовов допуска и всегда успешен; квитанция и готовность хранилища не проверяются.
func (admission *recordingAdmission) ServedStateReady(context.Context) error {
	admission.calls = append(admission.calls, "served-state-ready")
	return nil
}

// boolString maps the fixture boolean to its lowercase log spelling so flag-call assertions remain independent of production logging.
// boolString переводит эталонный bool в строчное написание журнала, чтобы проверки вызовов флагов не зависели от production-логирования.
func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
