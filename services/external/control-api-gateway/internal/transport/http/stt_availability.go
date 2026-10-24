package httptransport

import (
	"context"
	"net/http"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// speechAvailabilityTimeout bounds the standalone STT model-catalog RPC to five seconds,
	// independently of the longer context used for protected availability lease checks.
	// speechAvailabilityTimeout ограничивает самостоятельный STT model-catalog RPC пятью секундами
	// отдельно от более длинного контекста проверки защищённого availability lease.
	speechAvailabilityTimeout = 5 * time.Second
	// speechAvailabilityLeaseTimeout bounds the protected availability context to thirty seconds so a
	// reported short lease remains usable between realtime heartbeats. It does not extend owner or
	// provider authority.
	// speechAvailabilityLeaseTimeout ограничивает контекст защищённой availability тридцатью
	// секундами, чтобы короткий reported lease сохранялся между realtime heartbeat. Полномочия
	// владельца и провайдера не продлеваются.
	speechAvailabilityLeaseTimeout = 30 * time.Second
)

// writeBootstrapState validates and publicly maps the owner bootstrap state, localizes safe errors
// when available and augments it with the same protected speech-availability check used by
// realtime.
// writeBootstrapState проверяет и публично преобразует bootstrap state владельца, при наличии
// переводит безопасные ошибки и добавляет ту же защищённую проверку speech availability, что
// используется realtime.
func (server *Server) writeBootstrapState(w http.ResponseWriter, r *http.Request, state *controlplanev1.BootstrapState) {
	if state == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	value, err := messageMap(state)
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	if localizer, ok := w.(interface{ Localize(string) string }); ok {
		LocalizeSafeErrors(value, localizer.Localize)
	}
	value["speechTranscription"] = server.speechAvailability(r.Context(), state.GetSpeechTranscription())
	writeJSON(w, http.StatusOK, value)
}

// speechAvailability passes the attached STT client and owner eligibility snapshot to the shared
// availability checker under the caller context, without a separate HTTP-only readiness policy.
// speechAvailability передаёт подключённый STT клиент и снимок допуска владельца общему
// availability checker в контексте caller без отдельной HTTP политики готовности.
func (server *Server) speechAvailability(ctx context.Context, owner *controlplanev1.SpeechTranscriptionAvailability) generated.SpeechTranscriptionAvailability {
	return SpeechAvailability(ctx, server.speech, owner)
}

// SpeechAvailability first respects owner eligibility, then checks the protected STT path with a
// bounded lease context and maps known failure stages. READY requires an exact ready stage and a
// valid future lease no more than about thirty seconds ahead; HTTP bootstrap and WebSocket share
// this result.
// SpeechAvailability сначала учитывает допуск владельца, затем проверяет защищённый STT путь с
// ограниченным lease контекстом и отображает известные этапы отказа. READY требует точного ready
// этапа и корректного будущего lease не более примерно тридцати секунд; HTTP bootstrap и WebSocket
// используют общий результат.
func SpeechAvailability(ctx context.Context, speech sttv1.SpeechToTextServiceClient, owner *controlplanev1.SpeechTranscriptionAvailability) generated.SpeechTranscriptionAvailability {
	result := generated.SpeechTranscriptionAvailability{Reason: "STT_SERVICE_UNAVAILABLE"}
	if owner == nil || !owner.GetEligible() {
		result.Reason = unavailableSpeechReason(owner.GetReason())
		return result
	}
	if speech == nil {
		return result
	}
	// Realtime обновляет доступность раз в 15 секунд. Проверка получает
	// отдельный deadline, чтобы подтверждённый lease не истекал между двумя
	// heartbeat; фактический STT-вызов всё равно ограничен собственным timeout.
	ctx, cancel := context.WithTimeout(ctx, speechAvailabilityLeaseTimeout)
	defer cancel()
	availability, err := sttapi.CheckAvailability(ctx, speech)
	if err != nil || ctx.Err() != nil {
		if status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.Unauthenticated {
			result.Reason = "STT_PERMISSION_DENIED"
		}
		return result
	}
	if !availability.GetReady() {
		switch availability.GetStage() {
		case sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_POLICY:
			result.Reason = "STT_CONFIGURATION_UNAVAILABLE"
		case sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_CREDENTIAL:
			result.Reason = "STT_CREDENTIAL_UNAVAILABLE"
		case sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_EGRESS:
			result.Reason = "STT_EGRESS_UNAVAILABLE"
		case sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_PROVIDER:
			result.Reason = "STT_PROVIDER_UNAVAILABLE"
		}
		return result
	}
	now := time.Now()
	until := availability.GetValidUntil()
	if availability.GetStage() != sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY || until == nil || until.CheckValid() != nil || !until.AsTime().After(now) || until.AsTime().After(now.Add(31*time.Second)) {
		return result
	}
	validUntil := until.AsTime()
	result.Available, result.Reason, result.ValidUntil = true, "READY", &validUntil
	return result
}

// unavailableSpeechReason preserves only known owner-level STT configuration/permission/provider
// blockers; unknown text collapses to STT_SERVICE_UNAVAILABLE instead of exposing arbitrary
// diagnostic details.
// unavailableSpeechReason сохраняет только известные STT блокеры configuration/permission/provider
// владельца; неизвестный текст заменяется STT_SERVICE_UNAVAILABLE без произвольной диагностики.
func unavailableSpeechReason(reason string) generated.SpeechTranscriptionAvailabilityReason {
	switch reason {
	case "STT_NOT_CONFIGURED", "STT_DISABLED", "STT_PERMISSION_DENIED", "STT_PERMISSION_INVALID", "STT_PROVIDER_ACCOUNT_INELIGIBLE", "STT_PROVIDER_CREDENTIAL_UNSUPPORTED", "STT_PROVIDER_DISABLED", "STT_MODEL_UNSUPPORTED", "STT_CONFIGURATION_UNAVAILABLE":
		return generated.SpeechTranscriptionAvailabilityReason(reason)
	default:
		return "STT_SERVICE_UNAVAILABLE"
	}
}
