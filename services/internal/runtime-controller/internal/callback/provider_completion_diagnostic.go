package callback

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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
	server.logger.WarnContext(ctx, providerCompletionDiagnosticLog,
		"diagnostic_status", observed, "diagnostic_kind", kind, "provider_stage", stage,
		"provider_class", class, "provider_detail", detail, "notification", notification,
		"notification_error", notificationError, "account_read", accountRead, "terminal_code", terminal,
		"run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef,
		"turn_ref", input.TurnRef, "attempt", input.Attempt, "runtime_revision_digest", input.RuntimeRevisionDigest,
		"input_digest", input.InputDigest, "execution_binding_digest", input.ExecutionBindingDigest)
}
