package callback

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
)

// providerCompletionDiagnosticLog identifies a failure observation after a
// newly accepted owner completion, not an independent durable receipt.
// providerCompletionDiagnosticLog обозначает наблюдение ошибки после нового
// принятого владельцем завершения, а не отдельную устойчивую квитанцию.
const providerCompletionDiagnosticLog = "runtime provider failure observation after owner completion"

// logCommittedProviderDiagnostic records a failed completion only after a nil
// owner error; AlreadyExists is not new-commit evidence. Exact matching provider
// diagnostics and a bounded Pod-memory read precede caller-owned cleanup;
// missing evidence stays unavailable and the completion payload is unchanged.
// logCommittedProviderDiagnostic пишет ошибочное завершение лишь после nil ошибки
// владельца; AlreadyExists не доказывает новый commit. Точная диагностика
// провайдера и ограниченное чтение памяти Pod предшествуют очистке caller;
// отсутствие данных остаётся недоступностью, payload завершения не меняется.
func (server *Server) logCommittedProviderDiagnostic(ctx context.Context, input runtimecontract.RunnerInput, payload runtimecontract.RunnerCompletionRequest, ownerError error) {
	if ownerError != nil || payload.Success || server.logger == nil {
		return
	}
	observed, kind, stage, class, detail := "UNAVAILABLE", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"
	notification, notificationError, accountRead, terminal := "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"
	if diagnostic := payload.ProviderDiagnostic; diagnostic != nil && diagnostic.Matches(input) {
		observed, kind, stage, class, detail = "OBSERVED", diagnostic.Kind, diagnostic.Stage, diagnostic.Class, diagnostic.Detail
		notification, notificationError, accountRead, terminal = diagnostic.Notification, diagnostic.NotificationError, diagnostic.AccountRead, diagnostic.TerminalCode
	}
	podStatus, podMemory := server.committedPodMemoryObservation(ctx, input)
	server.logger.WarnContext(ctx, providerCompletionDiagnosticLog,
		"diagnostic_status", observed, "diagnostic_kind", kind, "provider_stage", stage,
		"provider_class", class, "provider_detail", detail, "notification", notification,
		"notification_error", notificationError, "account_read", accountRead, "terminal_code", terminal,
		"pod_memory_observation", podStatus, "pod_memory_reason", podMemory,
		"run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef,
		"turn_ref", input.TurnRef, "attempt", input.Attempt, "runtime_revision_digest", input.RuntimeRevisionDigest,
		"input_digest", input.InputDigest, "execution_binding_digest", input.ExecutionBindingDigest)
}

// committedPodMemoryObservation makes a one-second cancellation-independent
// read of the exact cold-turn Pod before cleanup, recognizing only its memory
// failure code. Missing manager, mismatches and read failures stay unavailable;
// warm-Pod history is not attributed to this execution.
// committedPodMemoryObservation за одну секунду независимо от отмены caller
// читает точный Pod холодного хода до очистки, распознавая лишь код ошибки памяти.
// Отсутствие manager, несовпадения и ошибки чтения остаются недоступностью;
// история warm Pod этому выполнению не приписывается.
func (server *Server) committedPodMemoryObservation(ctx context.Context, input runtimecontract.RunnerInput) (string, string) {
	if server.manager == nil {
		return "UNAVAILABLE", "UNKNOWN"
	}
	probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	observation, err := server.manager.ObserveTurnPod(probe, input, false)
	if err == nil && observation.State == "FAILED" && observation.DiagnosticCode == workload.TurnPodMemoryDiagnostic {
		return "OBSERVED", workload.TurnPodMemoryDiagnostic
	}
	return "UNAVAILABLE", "UNKNOWN"
}
