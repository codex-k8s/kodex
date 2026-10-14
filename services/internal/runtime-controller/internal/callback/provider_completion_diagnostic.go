package callback

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
)

const providerCompletionDiagnosticLog = "runtime provider failure observation after owner completion"

// Только nil owner error подтверждает новый completion. AlreadyExists не
// становится доказательством commit этой диагностики; cleanup не меняется.
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

// Read после нового owner commit и до cleanup не меняет semantic intent
// callback. История warm Pod не приписывается завершённому cold execution.
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
