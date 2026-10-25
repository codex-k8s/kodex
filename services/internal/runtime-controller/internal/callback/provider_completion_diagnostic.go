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
// diagnostics and a bounded current-Pod read precede caller-owned cleanup;
// missing evidence stays unavailable and the completion payload is unchanged.
// logCommittedProviderDiagnostic пишет ошибочное завершение лишь после nil ошибки
// владельца; AlreadyExists не доказывает новый commit. Точная диагностика
// провайдера и ограниченное чтение текущего Pod предшествуют очистке caller;
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
	podStatus, podReason, observedPodUID := server.committedPodFailureObservation(ctx, input)
	memoryStatus, memoryReason := "UNAVAILABLE", "UNKNOWN"
	if podReason == workload.TurnPodMemoryDiagnostic {
		memoryStatus, memoryReason = podStatus, podReason
	}
	server.logger.WarnContext(ctx, providerCompletionDiagnosticLog,
		"diagnostic_status", observed, "diagnostic_kind", kind, "provider_stage", stage,
		"provider_class", class, "provider_detail", detail, "notification", notification,
		"notification_error", notificationError, "account_read", accountRead, "terminal_code", terminal,
		"pod_memory_observation", memoryStatus, "pod_memory_reason", memoryReason,
		"pod_observation", podStatus, "pod_reason", podReason, "observed_pod_uid", observedPodUID,
		"run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef,
		"turn_ref", input.TurnRef, "attempt", input.Attempt, "runtime_revision_digest", input.RuntimeRevisionDigest,
		"input_digest", input.InputDigest, "execution_binding_digest", input.ExecutionBindingDigest)
}

// committedPodFailureObservation reads one current cold Pod within a cancellation-
// independent second before cleanup. Matching metadata and a nonempty observed UID
// permit only typed OOM/eviction diagnostics; neither history nor cause is inferred.
// It never reads the shared warm Pod or changes the immutable completion intent.
// committedPodFailureObservation читает текущий cold Pod за независимую от отмены
// секунду до очистки. Совпадение метаданных и непустой наблюдённый UID разрешают лишь
// типизированную диагностику OOM/выселения без вывода об истории либо причине.
// Общий warm Pod не читается; неизменяемый intent завершения не меняется.
func (server *Server) committedPodFailureObservation(ctx context.Context, input runtimecontract.RunnerInput) (string, string, string) {
	if server.manager == nil {
		return "UNAVAILABLE", "UNKNOWN", ""
	}
	probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	observation, err := server.manager.ObserveTurnPod(probe, input, false)
	if err == nil && observation.State == "FAILED" && observation.ObservedPodUID != "" &&
		(observation.DiagnosticCode == workload.TurnPodMemoryDiagnostic || observation.DiagnosticCode == workload.TurnPodEvictedDiagnostic) {
		return "OBSERVED", observation.DiagnosticCode, observation.ObservedPodUID
	}
	return "UNAVAILABLE", "UNKNOWN", ""
}
