package httptransport

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesFilePublicView reuses the shared closed metadata shape after strict protobuf validation.
// It never projects raw body, object coordinates, credentials, prepared receipts or execution credit.
// kubernetesFilePublicView использует shared closed metadata shape после strict protobuf validation.
// Raw body, object coordinates, credentials, prepared receipts либо execution credit не раскрывает.
func kubernetesFilePublicView(v *cp.ManagedKubernetesFileInputView) (generated.KubernetesFileInputView, bool) {
	var empty generated.KubernetesFileInputView
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 || v.GetCreatedAt() == nil || v.GetHardDeadline() == nil || v.GetCreatedAt().CheckValid() != nil || v.GetHardDeadline().CheckValid() != nil || len(v.GetCreatedAt().ProtoReflect().GetUnknown()) != 0 || len(v.GetHardDeadline().ProtoReflect().GetUnknown()) != 0 {
		return empty, false
	}
	state, known := cp.ManagedKubernetesFileInputState_name[int32(v.GetState())]
	reason, knownReason := cp.ManagedKubernetesFileInputReason_name[int32(v.GetReason())]
	if !known || !knownReason {
		return empty, false
	}
	metadata := managedkubernetes.FileInputView{SourceRef: v.GetSourceRef(), InvocationRef: v.GetInvocationRef(), PlanRef: v.GetPlanRef(), ChildRef: v.GetChildRef(), StreamRef: v.GetStreamRef(), State: strings.TrimPrefix(state, "MANAGED_KUBERNETES_FILE_INPUT_STATE_"), Version: v.GetVersion(), ArtifactRef: v.GetArtifactRef(), ArtifactRevisionRef: v.GetArtifactRevisionRef(), ArtifactSHA256: v.GetArtifactSha256(), ArtifactSizeBytes: v.GetArtifactSizeBytes(), TransferFormat: v.GetTransferFormat(), NormalizedSHA256: v.GetNormalizedSha256(), NormalizedSizeBytes: v.GetNormalizedSizeBytes(), NormalizationRevision: v.GetNormalizationRevision(), StagedBytes: v.GetStagedBytes(), AcknowledgedBytes: v.GetAcknowledgedBytes(), EOFAcknowledged: v.GetEofAcknowledged(), Reason: strings.TrimPrefix(reason, "MANAGED_KUBERNETES_FILE_INPUT_REASON_"), CreatedAt: v.GetCreatedAt().AsTime(), HardDeadline: v.GetHardDeadline().AsTime()}
	if !metadata.Valid() || metadata.Version > 9007199254740991 {
		return empty, false
	}
	var cleanup *generated.KubernetesStagingCleanupStatus
	if v.Cleanup != nil {
		parsed, valid := kubernetesFileCleanupPublicStatus(v.Cleanup)
		if !valid {
			return empty, false
		}
		cleanup = &parsed
	}
	return generated.KubernetesFileInputView{SourceRef: metadata.SourceRef, InvocationRef: metadata.InvocationRef, PlanRef: metadata.PlanRef, ChildRef: metadata.ChildRef, StreamRef: metadata.StreamRef, State: generated.KubernetesFileInputViewState(metadata.State), Version: metadata.Version, ArtifactRef: metadata.ArtifactRef, ArtifactRevisionRef: metadata.ArtifactRevisionRef, ArtifactSHA256: metadata.ArtifactSHA256, ArtifactSizeBytes: int64(metadata.ArtifactSizeBytes), TransferFormat: generated.KubernetesFileInputViewTransferFormat(metadata.TransferFormat), NormalizedSHA256: metadata.NormalizedSHA256, NormalizedSizeBytes: int64(metadata.NormalizedSizeBytes), NormalizationRevision: int(metadata.NormalizationRevision), StagedBytes: int64(metadata.StagedBytes), AcknowledgedBytes: int64(metadata.AcknowledgedBytes), EofAcknowledged: metadata.EOFAcknowledged, Reason: generated.KubernetesFileInputViewReason(metadata.Reason), CreatedAt: metadata.CreatedAt, HardDeadline: metadata.HardDeadline, Cleanup: cleanup}, true
}

// kubernetesFileClientAvailable closes an absent typed owner port without a generic artifact fallback.
// kubernetesFileClientAvailable закрывает missing typed owner port без generic artifact fallback.
func (server *Server) kubernetesFileClientAvailable(w http.ResponseWriter) bool {
	if server.control == nil || server.control.KubernetesFiles == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return false
	}
	return true
}

// ListManagedKubernetesFileInputs returns only safe metadata for the authenticated original invocation.
// No first-artifact fallback, preparation command or input bytes endpoint exists on this route.
// ListManagedKubernetesFileInputs возвращает лишь safe metadata authenticated original invocation.
// First-artifact fallback, preparation command либо input bytes endpoint у route отсутствует.
func (server *Server) ListManagedKubernetesFileInputs(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, params generated.ListManagedKubernetesFileInputsParams) {
	w.Header().Set("Cache-Control", "no-store")
	only, _, valid := kubernetesFileHTTPProjection(r, params.CleanupOnly, nil, false)
	if !validKubernetesInputHTTPRef(inv, "inv_") || !valid {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesFileClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesFiles.ListManagedKubernetesFileInputs(r.Context(), &cp.ListManagedKubernetesFileInputsRequest{InvocationRef: inv, CleanupOnly: only})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	seen := map[string]bool{}
	if only {
		if response.Projection != cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY || len(response.Items) != 0 || len(response.CleanupItems) > 256 {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result := generated.KubernetesFileCleanupList{Projection: "CLEANUP_ONLY", Items: make([]generated.KubernetesFileCleanupView, 0, len(response.CleanupItems))}
		for _, item := range response.CleanupItems {
			view, valid := kubernetesFileCleanupPublicView(item)
			if !valid || view.InvocationRef != inv || seen[view.SourceRef] {
				writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
			seen[view.SourceRef] = true
			result.Items = append(result.Items, view)
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if response.Projection != cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE || len(response.CleanupItems) != 0 || len(response.Items) > 256 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesFileSourceList{Projection: "SOURCE", Items: make([]generated.KubernetesFileInputView, 0, len(response.Items))}
	for _, item := range response.Items {
		view, valid := kubernetesFilePublicView(item)
		if !valid || view.InvocationRef != inv || seen[view.SourceRef] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[view.SourceRef] = true
		result.Items = append(result.Items, view)
	}
	writeJSON(w, http.StatusOK, result)
}

// GetManagedKubernetesFileInput requires an explicit projection and never substitutes source provenance after a cleanup-only request.
// GetManagedKubernetesFileInput требует explicit projection и не подставляет source provenance после cleanup-only request.
func (server *Server) GetManagedKubernetesFileInput(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, source string, params generated.GetManagedKubernetesFileInputParams) {
	w.Header().Set("Cache-Control", "no-store")
	only, key, valid := kubernetesFileHTTPProjection(r, params.CleanupOnly, params.CleanupIdempotencyKey, true)
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(source, "kfs_") || !valid {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesFileClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesFiles.GetManagedKubernetesFileInput(r.Context(), &cp.GetManagedKubernetesFileInputRequest{InvocationRef: inv, SourceRef: source, CleanupOnly: only, CleanupIdempotencyKey: key})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if only {
		if response.Projection != cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY || response.View != nil || key == "" && response.CleanupReceipt != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result, valid := kubernetesFileCleanupPublicReadback(response.CleanupView, response.CleanupReceipt)
		if !valid || result.CleanupView.InvocationRef != inv || result.CleanupView.SourceRef != source {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(result.CleanupView.Cleanup.Version, 10)))
		writeJSON(w, http.StatusOK, result)
		return
	}
	view, valid := kubernetesFilePublicView(response.View)
	if response.Projection != cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE || response.CleanupView != nil || response.CleanupReceipt != nil || !valid || view.InvocationRef != inv || view.SourceRef != source {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.KubernetesFileSourceReadback{Projection: "SOURCE", View: view})
}

// kubernetesFileHTTPProjection rejects duplicate, noncanonical and unknown query fields before an owner RPC.
// The mode chooses metadata shape only, never source or mutation authority.
// kubernetesFileHTTPProjection отклоняет duplicate, noncanonical и unknown query fields до owner RPC.
// Mode выбирает лишь metadata shape, не source либо mutation authority.
func kubernetesFileHTTPProjection(r *http.Request, flag *bool, receiptKey *string, allowKey bool) (bool, string, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false, "", false
	}
	for key, items := range values {
		if len(items) != 1 || key != "cleanupOnly" && (key != "cleanupIdempotencyKey" || !allowKey) {
			return false, "", false
		}
	}
	only := flag != nil && *flag
	if values.Has("cleanupOnly") && (values.Get("cleanupOnly") != "true" && values.Get("cleanupOnly") != "false" || values.Get("cleanupOnly") != strconv.FormatBool(only)) {
		return false, "", false
	}
	key := ""
	if receiptKey != nil {
		key = *receiptKey
	}
	if values.Has("cleanupIdempotencyKey") && (key == "" || key != values.Get("cleanupIdempotencyKey")) {
		return false, "", false
	}
	if key != "" && (!only || !validKubernetesInputHTTPKey(key)) {
		return false, "", false
	}
	return only, key, true
}

// kubernetesFileCleanupPublicStatus reuses canonical counter validation and the one-object FILE budget.
// kubernetesFileCleanupPublicStatus использует canonical counter validation и one-object FILE budget.
func kubernetesFileCleanupPublicStatus(v *cp.ManagedKubernetesStagingCleanupStatus) (generated.KubernetesStagingCleanupStatus, bool) {
	status, valid := kubernetesCleanupPublicStatus(v)
	return status, valid && status.OutstandingCount <= 1 && status.OutstandingBytes <= 16<<20
}

// kubernetesFileCleanupPublicView has no representable artifact, hash, filename, manifest or private locator field.
// kubernetesFileCleanupPublicView не содержит representable artifact/hash/filename/manifest/private locator field.
func kubernetesFileCleanupPublicView(v *cp.ManagedKubernetesFileCleanupView) (generated.KubernetesFileCleanupView, bool) {
	var empty generated.KubernetesFileCleanupView
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInputHTTPRef(v.SourceRef, "kfs_") || !validKubernetesInputHTTPRef(v.InvocationRef, "inv_") {
		return empty, false
	}
	cleanup, valid := kubernetesFileCleanupPublicStatus(v.Cleanup)
	if !valid {
		return empty, false
	}
	view := generated.KubernetesFileCleanupView{SourceRef: v.SourceRef, InvocationRef: v.InvocationRef, Cleanup: cleanup}
	if kubernetesFileCleanupRepairEnabled {
		available := true
		view.RepairAvailable = &available
	}
	return view, true
}

// kubernetesFileCleanupPublicReadback binds the original-key receipt to current minimal counters without claiming deletion.
// kubernetesFileCleanupPublicReadback связывает original-key receipt с current minimal counters без утверждения deletion.
func kubernetesFileCleanupPublicReadback(v *cp.ManagedKubernetesFileCleanupView, r *cp.ManagedKubernetesFileCleanupRepairReceipt) (generated.KubernetesFileCleanupReadback, bool) {
	view, valid := kubernetesFileCleanupPublicView(v)
	result := generated.KubernetesFileCleanupReadback{Projection: "CLEANUP_ONLY", CleanupView: view}
	if !valid {
		return result, false
	}
	if r == nil {
		return result, true
	}
	if len(r.ProtoReflect().GetUnknown()) != 0 || r.SourceRef != view.SourceRef || r.Version < 2 || r.Version > view.Cleanup.Version || r.RepairGeneration < 1 || r.RepairGeneration > view.Cleanup.RepairGeneration || r.RepairedCount != 1 || r.RemainingExhaustedCount < 0 || r.RemainingExhaustedCount > 1 || r.AcceptedAt == nil || len(r.AcceptedAt.ProtoReflect().GetUnknown()) != 0 || r.AcceptedAt.CheckValid() != nil || r.AcceptedAt.AsTime().IsZero() || r.AcceptedAt.AsTime().After(time.Now().Add(30*time.Second)) {
		return result, false
	}
	result.CleanupReceipt = &generated.KubernetesFileCleanupRepairReceipt{SourceRef: r.SourceRef, Version: r.Version, RepairGeneration: r.RepairGeneration, RepairedCount: r.RepairedCount, RemainingExhaustedCount: r.RemainingExhaustedCount, AcceptedAt: r.AcceptedAt.AsTime()}
	return result, true
}

// kubernetesFileCleanupRepairEnabled records whether the released gateway can call the owner method.
// kubernetesFileCleanupRepairEnabled показывает, может ли выпущенный шлюз вызвать метод владельца.
var kubernetesFileCleanupRepairEnabled = controlplaneclient.ControlAPIGatewayOperations()[controlplaneclient.KubernetesFileCleanupRepairOperation] != ""

// RepairManagedKubernetesFileCleanup submits one bounded closure-only command; lost ACK is recovered by exact-key GET.
// It accepts no source body, actor, storage key, frame, native grant or retry instruction.
// RepairManagedKubernetesFileCleanup передаёт bounded closure-only command; lost ACK восстанавливается exact-key GET.
// Source body, actor, storage key, frame, native grant либо retry instruction не принимает.
func (server *Server) RepairManagedKubernetesFileCleanup(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, source string, params generated.RepairManagedKubernetesFileCleanupParams) {
	w.Header().Set("Cache-Control", "no-store")
	if !kubernetesFileCleanupRepairEnabled {
		writeLocalProblem(w, http.StatusServiceUnavailable, "KUBERNETES_FILE_CLEANUP_REPAIR_UNAVAILABLE", false)
		return
	}
	server.repairManagedKubernetesFileCleanup(w, r, inv, source, params)
}

// repairManagedKubernetesFileCleanup validates the bounded request and checks the resulting owner receipt.
// repairManagedKubernetesFileCleanup проверяет ограниченный запрос и квитанцию владельца.
func (server *Server) repairManagedKubernetesFileCleanup(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, source string, params generated.RepairManagedKubernetesFileCleanupParams) {
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(source, "kfs_") || r.URL.RawQuery != "" {
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
	if !server.kubernetesFileClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesFiles.RepairManagedKubernetesFileCleanup(r.Context(), &cp.RepairManagedKubernetesFileCleanupRequest{InvocationRef: inv, SourceRef: source, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.Projection != cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result, valid := kubernetesFileCleanupPublicReadback(response.CleanupView, response.CleanupReceipt)
	if !valid || result.CleanupReceipt == nil || result.CleanupView.InvocationRef != inv || result.CleanupView.SourceRef != source {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(result.CleanupView.Cleanup.Version, 10)))
	writeJSON(w, http.StatusOK, result)
}
