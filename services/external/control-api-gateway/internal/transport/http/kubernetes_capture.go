package httptransport

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesCapturePublicSummary accepts only current safe metadata, never a task grant or private locator.
// kubernetesCapturePublicSummary принимает лишь current safe metadata, не task grant либо private locator.
func kubernetesCapturePublicSummary(v *cp.ManagedKubernetesCaptureSummary) (generated.KubernetesCaptureSummary, bool) {
	var out generated.KubernetesCaptureSummary
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInputHTTPRef(v.CaptureRef, "koc_") || !validKubernetesInputHTTPRef(v.StreamRef, "kst_") || !validManagedVersion(v.Version) || !validManagedVersion(v.CaptureGeneration) || !managedkubernetes.ValidCaptureReason(v.ReasonCode) {
		return out, false
	}
	state, known := cp.ManagedKubernetesCaptureState_name[int32(v.State)]
	if !known || v.State == cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_UNSPECIFIED {
		return out, false
	}
	return generated.KubernetesCaptureSummary{CaptureRef: v.CaptureRef, StreamRef: v.StreamRef, CaptureGeneration: v.CaptureGeneration, Version: v.Version, State: generated.KubernetesCaptureSummaryState(strings.TrimPrefix(state, "MANAGED_KUBERNETES_CAPTURE_STATE_")), ReasonCode: generated.KubernetesCaptureSummaryReasonCode(v.ReasonCode)}, true
}

// kubernetesCapturePublicReadback validates the complete metadata union before any public response.
// It does not turn COMPLETE/PARTIAL into native success or artifact download authorization.
// kubernetesCapturePublicReadback проверяет полный metadata union до public response.
// COMPLETE/PARTIAL не становятся native success либо artifact download authorization.
func kubernetesCapturePublicReadback(v *cp.ManagedKubernetesCaptureStatus, r *cp.ManagedKubernetesCaptureRepairReceipt) (generated.KubernetesCaptureReadback, bool) {
	var out generated.KubernetesCaptureReadback
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 {
		return out, false
	}
	summary, valid := kubernetesCapturePublicSummary(v.Summary)
	if !valid {
		return out, false
	}
	operation, known := integrationpackage.KubernetesOperationByKey(v.Operation)
	if !known || operation.Execution != integrationpackage.KubernetesStream || !validKubernetesInputHTTPRef(v.InvocationRef, "inv_") || !validKubernetesInputHTTPRef(v.RunRef, "run_") || !validKubernetesInputHTTPRef(v.NodeRef, "nod_") || v.AttemptCount < 0 || v.AttemptCount > maximumSafeJSONInteger || v.RepairGeneration < 0 || v.RepairGeneration > maximumSafeJSONInteger || v.SealedAt == nil || len(v.SealedAt.ProtoReflect().GetUnknown()) != 0 || v.SealedAt.CheckValid() != nil || v.SealedAt.AsTime().IsZero() || len(v.Artifacts) > 5 {
		return out, false
	}
	kind := "NONE"
	if summary.State == "COMPLETE" {
		name, known := cp.ManagedKubernetesCaptureKind_name[int32(v.Kind)]
		if !known || v.Kind == cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_UNSPECIFIED {
			return out, false
		}
		kind = strings.TrimPrefix(name, "MANAGED_KUBERNETES_CAPTURE_KIND_")
		if kind == "EMPTY" && len(v.Artifacts) != 0 || kind == "FULL" && len(v.Artifacts) == 0 {
			return out, false
		}
	} else if v.Kind != cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_UNSPECIFIED || len(v.Artifacts) != 0 {
		return out, false
	}
	out.View = generated.KubernetesCaptureStatus{Summary: summary, InvocationRef: v.InvocationRef, RunRef: v.RunRef, NodeRef: v.NodeRef, Operation: v.Operation, Kind: generated.KubernetesCaptureStatusKind(kind), AttemptCount: v.AttemptCount, RepairGeneration: v.RepairGeneration, SealedAt: v.SealedAt.AsTime(), Artifacts: make([]generated.KubernetesCapturedArtifact, 0, len(v.Artifacts))}
	if v.RetainUntil != nil {
		if len(v.RetainUntil.ProtoReflect().GetUnknown()) != 0 || v.RetainUntil.CheckValid() != nil || !v.RetainUntil.AsTime().After(v.SealedAt.AsTime()) {
			return out, false
		}
		until := v.RetainUntil.AsTime()
		out.View.RetainUntil = &until
	}
	seen := map[string]bool{}
	for _, a := range v.Artifacts {
		if a == nil || len(a.ProtoReflect().GetUnknown()) != 0 || seen[a.Channel] || !validKubernetesInputHTTPRef(a.ArtifactRef, "art_") || !validKubernetesInputHTTPRef(a.RevisionRef, "arv_") || (!strings.HasPrefix(a.Digest, "sha256:") || !managedkubernetes.ValidDigest(strings.TrimPrefix(a.Digest, "sha256:"))) || a.SizeBytes < 0 || a.SizeBytes > 536870912 || a.CaptureGeneration != summary.CaptureGeneration || kind == "FULL" && a.Partial {
			return out, false
		}
		switch a.Channel {
		case "STDOUT", "STDERR", "CONTROL", "FILE_CONTENT", "PORT_FORWARD":
		default:
			return out, false
		}
		seen[a.Channel] = true
		out.View.Artifacts = append(out.View.Artifacts, generated.KubernetesCapturedArtifact{Channel: generated.KubernetesCapturedArtifactChannel(a.Channel), ArtifactRef: a.ArtifactRef, RevisionRef: a.RevisionRef, Digest: a.Digest, SizeBytes: a.SizeBytes, CaptureGeneration: a.CaptureGeneration, Partial: a.Partial})
	}
	if r != nil {
		if len(r.ProtoReflect().GetUnknown()) != 0 || r.CaptureRef != summary.CaptureRef || r.RepairGeneration < 1 || r.RepairGeneration > v.RepairGeneration || r.Version < 2 || r.Version > summary.Version || r.AttemptCount < 0 || r.AttemptCount > v.AttemptCount || r.AcceptedAt == nil || len(r.AcceptedAt.ProtoReflect().GetUnknown()) != 0 || r.AcceptedAt.CheckValid() != nil || r.AcceptedAt.AsTime().Before(v.SealedAt.AsTime()) {
			return out, false
		}
		out.RepairReceipt = &generated.KubernetesCaptureRepairReceipt{CaptureRef: r.CaptureRef, RepairGeneration: r.RepairGeneration, Version: r.Version, AttemptCount: r.AttemptCount, AcceptedAt: r.AcceptedAt.AsTime()}
	}
	if v.Cleanup != nil {
		cleanup, valid := kubernetesCleanupPublicStatus(v.Cleanup)
		if !valid {
			return out, false
		}
		out.View.Cleanup = &cleanup
	}
	return out, true
}

// kubernetesCaptureClientAvailable refuses absent typed composition without a generic retry fallback.
// kubernetesCaptureClientAvailable отклоняет отсутствие typed composition без generic retry fallback.
func (server *Server) kubernetesCaptureClientAvailable(w http.ResponseWriter) bool {
	if server.control == nil || server.control.KubernetesCaptures == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return false
	}
	return true
}

// GetManagedKubernetesCaptureStatus reads current original-owner history and an optional original-key receipt.
// GetManagedKubernetesCaptureStatus читает current original-owner history и optional original-key receipt.
func (server *Server) GetManagedKubernetesCaptureStatus(w http.ResponseWriter, r *http.Request, captureRef string, params generated.GetManagedKubernetesCaptureStatusParams) {
	cleanupKey := ""
	if params.CleanupIdempotencyKey != nil {
		cleanupKey = *params.CleanupIdempotencyKey
	}
	if cleanupKey != "" && !validKubernetesInputHTTPKey(cleanupKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	key := ""
	if params.RepairIdempotencyKey != nil {
		key = *params.RepairIdempotencyKey
	}
	if !validKubernetesInputHTTPRef(captureRef, "koc_") || key != "" && !validKubernetesInputHTTPKey(key) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesCaptureClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesCaptures.GetManagedKubernetesCaptureStatus(r.Context(), &cp.GetManagedKubernetesCaptureStatusRequest{CaptureRef: captureRef, RepairIdempotencyKey: key, CleanupIdempotencyKey: cleanupKey})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesCapturePublicReadback(response.View, response.RepairReceipt)
	if !valid || view.View.Summary.CaptureRef != captureRef || key == "" && view.RepairReceipt != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if !kubernetesCleanupPublicReceipt(&view, response.CleanupReceipt) || cleanupKey == "" && view.CleanupReceipt != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.View.Summary.Version, 10)))
	writeJSON(w, http.StatusOK, view)
}

// RepairManagedKubernetesCapture accepts an empty purpose command, exact OCC and idempotency.
// An uncertain result is reconciled with one Get(original key); no mutation retry is performed here.
// RepairManagedKubernetesCapture принимает пустую purpose command, exact OCC и idempotency.
// Неизвестный результат сверяется одним Get(original key); mutation retry здесь отсутствует.
func (server *Server) RepairManagedKubernetesCapture(w http.ResponseWriter, r *http.Request, captureRef string, params generated.RepairManagedKubernetesCaptureParams) {
	if !validKubernetesInputHTTPRef(captureRef, "koc_") || !validKubernetesInputHTTPKey(params.IdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, valid := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !valid {
		return
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || params.IfMatch != strconv.Quote(strconv.FormatInt(*mutation.ExpectedVersion, 10)) || r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	defer clear(raw)
	var body struct{}
	if err != nil || len(raw) > 1024 || internalrpcauth.DecodeExactJSON(raw, &body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesCaptureClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesCaptures.RepairManagedKubernetesCapture(r.Context(), &cp.RepairManagedKubernetesCaptureRequest{CaptureRef: captureRef, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesCapturePublicReadback(response.View, response.RepairReceipt)
	if !valid || view.RepairReceipt == nil || view.View.Summary.CaptureRef != captureRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.View.Summary.Version, 10)))
	writeJSON(w, http.StatusOK, view)
}

// kubernetesCleanupPublicStatus validates the owner cleanup projection and contains no object pins.
// kubernetesCleanupPublicStatus проверяет owner cleanup projection без object pins.
func kubernetesCleanupPublicStatus(v *cp.ManagedKubernetesStagingCleanupStatus) (generated.KubernetesStagingCleanupStatus, bool) {
	var out generated.KubernetesStagingCleanupStatus
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 || !validManagedVersion(v.Version) || v.RepairGeneration < 0 || v.RepairGeneration > maximumSafeJSONInteger || v.OutstandingCount < 0 || v.OutstandingCount > maximumSafeJSONInteger || v.OutstandingBytes < 0 || v.OutstandingBytes > maximumSafeJSONInteger || v.ExhaustedCount < 0 || v.ExhaustedCount > v.OutstandingCount || v.EligibleExhaustedCount < 0 || v.EligibleExhaustedCount > v.ExhaustedCount {
		return out, false
	}
	return generated.KubernetesStagingCleanupStatus{Version: v.Version, RepairGeneration: v.RepairGeneration, OutstandingCount: v.OutstandingCount, OutstandingBytes: v.OutstandingBytes, ExhaustedCount: v.ExhaustedCount, EligibleExhaustedCount: v.EligibleExhaustedCount}, true
}

// kubernetesCleanupPublicReceipt binds one immutable cleanup ACK to the currently visible capture.
// kubernetesCleanupPublicReceipt связывает immutable cleanup ACK с current visible capture.
func kubernetesCleanupPublicReceipt(view *generated.KubernetesCaptureReadback, r *cp.ManagedKubernetesStagingCleanupRepairReceipt) bool {
	if r == nil {
		return true
	}
	c := view.View.Cleanup
	if c == nil || len(r.ProtoReflect().GetUnknown()) != 0 || r.CaptureRef != view.View.Summary.CaptureRef || r.Version < 2 || r.Version > c.Version || r.RepairGeneration < 1 || r.RepairGeneration > c.RepairGeneration || r.RepairedCount < 1 || r.RepairedCount > 100 || r.RemainingExhaustedCount < 0 || r.RemainingExhaustedCount > maximumSafeJSONInteger || r.AcceptedAt == nil || len(r.AcceptedAt.ProtoReflect().GetUnknown()) != 0 || r.AcceptedAt.CheckValid() != nil || r.AcceptedAt.AsTime().Before(view.View.SealedAt) {
		return false
	}
	view.CleanupReceipt = &generated.KubernetesStagingCleanupRepairReceipt{CaptureRef: r.CaptureRef, Version: r.Version, RepairGeneration: r.RepairGeneration, RepairedCount: r.RepairedCount, RemainingExhaustedCount: r.RemainingExhaustedCount, AcceptedAt: r.AcceptedAt.AsTime()}
	return true
}

// RepairManagedKubernetesStagingCleanup opens only an exhausted storage cleanup cycle with exact OCC.
// Unknown ACK is read back through Get(cleanup key), never by resubmitting object effects.
// RepairManagedKubernetesStagingCleanup открывает exhausted storage cleanup cycle с exact OCC.
// Unknown ACK сверяется Get(cleanup key), не повторной отправкой object effects.
func (server *Server) RepairManagedKubernetesStagingCleanup(w http.ResponseWriter, r *http.Request, captureRef string, params generated.RepairManagedKubernetesStagingCleanupParams) {
	if !validKubernetesInputHTTPRef(captureRef, "koc_") || !validKubernetesInputHTTPKey(params.IdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, valid := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !valid {
		return
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || params.IfMatch != strconv.Quote(strconv.FormatInt(*mutation.ExpectedVersion, 10)) || r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	defer clear(raw)
	var body struct{}
	if err != nil || len(raw) > 1024 || internalrpcauth.DecodeExactJSON(raw, &body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesCaptureClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesCaptures.RepairManagedKubernetesStagingCleanup(r.Context(), &cp.RepairManagedKubernetesStagingCleanupRequest{CaptureRef: captureRef, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesCapturePublicReadback(response.View, nil)
	if !valid || !kubernetesCleanupPublicReceipt(&view, response.CleanupReceipt) || view.CleanupReceipt == nil || view.View.Summary.CaptureRef != captureRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.View.Cleanup.Version, 10)))
	writeJSON(w, http.StatusOK, view)
}
