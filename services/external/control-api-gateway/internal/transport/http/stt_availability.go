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

const speechAvailabilityTimeout = 5 * time.Second

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

func (server *Server) speechAvailability(ctx context.Context, owner *controlplanev1.SpeechTranscriptionAvailability) generated.SpeechTranscriptionAvailability {
	result := generated.SpeechTranscriptionAvailability{Reason: "STT_SERVICE_UNAVAILABLE"}
	if owner == nil || !owner.GetEligible() {
		result.Reason = unavailableSpeechReason(owner.GetReason())
		return result
	}
	if server.speech == nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, speechAvailabilityTimeout)
	defer cancel()
	availability, err := sttapi.CheckAvailability(ctx, server.speech)
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

func unavailableSpeechReason(reason string) generated.SpeechTranscriptionAvailabilityReason {
	switch reason {
	case "STT_NOT_CONFIGURED", "STT_DISABLED", "STT_PERMISSION_DENIED", "STT_PERMISSION_INVALID", "STT_PROVIDER_ACCOUNT_INELIGIBLE", "STT_PROVIDER_CREDENTIAL_UNSUPPORTED", "STT_PROVIDER_DISABLED", "STT_MODEL_UNSUPPORTED", "STT_CONFIGURATION_UNAVAILABLE":
		return generated.SpeechTranscriptionAvailabilityReason(reason)
	default:
		return "STT_SERVICE_UNAVAILABLE"
	}
}
