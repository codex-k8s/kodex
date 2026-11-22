package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fileHTTPFixture supplies historical metadata only; READY never proves a native copy or grant.
// fileHTTPFixture задаёт historical metadata; READY не доказывает native copy либо grant.
func fileHTTPFixture() *cp.ManagedKubernetesFileInputView {
	return &cp.ManagedKubernetesFileInputView{SourceRef: "kfs_synthetic", InvocationRef: "inv_synthetic", PlanRef: "kpl_synthetic", ChildRef: "kch_synthetic", StreamRef: "kst_synthetic", State: cp.ManagedKubernetesFileInputState_MANAGED_KUBERNETES_FILE_INPUT_STATE_READY, Version: 2, ArtifactRef: "art_synthetic", ArtifactRevisionRef: "arv_synthetic", ArtifactSha256: strings.Repeat("a", 64), ArtifactSizeBytes: 4, TransferFormat: "FILE", NormalizedSha256: strings.Repeat("b", 64), NormalizedSizeBytes: 2048, NormalizationRevision: 1, StagedBytes: 1024, Reason: cp.ManagedKubernetesFileInputReason_MANAGED_KUBERNETES_FILE_INPUT_REASON_NONE, CreatedAt: timestamppb.New(time.Unix(1700000000, 0)), HardDeadline: timestamppb.New(time.Unix(1700000600, 0))}
}

// fileHTTPHandler exercises generated routes and the production typed client without simulating authentication.
// fileHTTPHandler проверяет generated routes и production typed client без имитации authentication.
func fileHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesFiles: cp.NewManagedKubernetesFileServiceClient(recorder)}})
}

// TestKubernetesFileCleanupRepairUnavailable verifies the released route fails before an owner RPC.
// TestKubernetesFileCleanupRepairUnavailable проверяет отказ опубликованного маршрута до RPC владельца.
func TestKubernetesFileCleanupRepairUnavailable(t *testing.T) {
	const path = "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs/kfs_synthetic/cleanup-repair"
	recorder := &catalogRPCRecorder{}
	w := httptest.NewRecorder()
	fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path, "{}"))
	if w.Code != http.StatusServiceUnavailable || recorder.request != nil || !strings.Contains(w.Body.String(), "KUBERNETES_FILE_CLEANUP_REPAIR_UNAVAILABLE") {
		t.Fatal("unpublished FILE cleanup repair reached the owner", w.Code)
	}
	view, valid := kubernetesFileCleanupPublicView(fileCleanupHTTPFixture())
	if !valid || view.RepairAvailable != nil {
		t.Fatal("cleanup readback advertised an unpublished command")
	}
}

// TestKubernetesFileHTTPMetadataRoutes binds safe List/Get to exact invocation/source before any body is returned.
// TestKubernetesFileHTTPMetadataRoutes связывает safe List/Get с exact invocation/source до выдачи body.
func TestKubernetesFileHTTPMetadataRoutes(t *testing.T) {
	const base = "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs"
	view := fileHTTPFixture()
	for _, item := range []struct {
		path, method string
		response     proto.Message
	}{
		{base, cp.ManagedKubernetesFileService_ListManagedKubernetesFileInputs_FullMethodName, &cp.ListManagedKubernetesFileInputsResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, Items: []*cp.ManagedKubernetesFileInputView{view}}},
		{base + "/kfs_synthetic", cp.ManagedKubernetesFileService_GetManagedKubernetesFileInput_FullMethodName, &cp.GetManagedKubernetesFileInputResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, View: view}},
	} {
		recorder := &catalogRPCRecorder{response: item.response}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, item.path, ""))
		if w.Code != 200 || recorder.method != item.method || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"artifactRevisionRef":"arv_synthetic"`) {
			t.Fatal("FILE metadata route lost exact provenance", w.Code)
		}
		if strings.Contains(w.Body.String(), "objectKey") || strings.Contains(w.Body.String(), "payload") || strings.Contains(w.Body.String(), "authorizedUntil") {
			t.Fatal("private FILE material disclosed")
		}
	}
	for _, path := range []string{base + "?artifactRef=art_foreign", base + "/kfs_synthetic?prepare=true"} {
		recorder := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path, ""))
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("caller FILE selector reached owner", w.Code)
		}
	}
	for _, mutate := range []func(*cp.ManagedKubernetesFileInputView){
		func(v *cp.ManagedKubernetesFileInputView) { v.InvocationRef = "inv_foreign00" }, func(v *cp.ManagedKubernetesFileInputView) { v.SourceRef = "kfs_foreign00" }, func(v *cp.ManagedKubernetesFileInputView) { v.State = 99 }, func(v *cp.ManagedKubernetesFileInputView) { v.Reason = 0 }, func(v *cp.ManagedKubernetesFileInputView) { v.Version = 1 << 53 }, func(v *cp.ManagedKubernetesFileInputView) { v.EofAcknowledged = true }, func(v *cp.ManagedKubernetesFileInputView) { v.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) },
	} {
		changed := proto.Clone(view).(*cp.ManagedKubernetesFileInputView)
		mutate(changed)
		recorder := &catalogRPCRecorder{response: &cp.GetManagedKubernetesFileInputResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, View: changed}}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, base+"/kfs_synthetic", ""))
		if w.Code != 502 {
			t.Fatal("invalid FILE response disclosed", w.Code)
		}
	}
	recorder := &catalogRPCRecorder{response: &cp.ListManagedKubernetesFileInputsResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, Items: []*cp.ManagedKubernetesFileInputView{view, view}}}
	w := httptest.NewRecorder()
	fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, base, ""))
	if w.Code != 502 {
		t.Fatal("duplicate FILE source admitted", w.Code)
	}
}

// fileCleanupHTTPFixture contains only original-root cleanup counters, with no file provenance or bytes.
// fileCleanupHTTPFixture содержит только original-root cleanup counters, без provenance либо file bytes.
func fileCleanupHTTPFixture() *cp.ManagedKubernetesFileCleanupView {
	return &cp.ManagedKubernetesFileCleanupView{SourceRef: "kfs_synthetic", InvocationRef: "inv_synthetic", Cleanup: &cp.ManagedKubernetesStagingCleanupStatus{Version: 3, OutstandingCount: 1, OutstandingBytes: 2048, ExhaustedCount: 1, EligibleExhaustedCount: 1}}
}

// TestKubernetesFileCleanupHTTPProjection proves the real route cannot substitute full source metadata into closure-only reads.
// TestKubernetesFileCleanupHTTPProjection проверяет, что actual route не подставляет full source metadata в closure-only reads.
func TestKubernetesFileCleanupHTTPProjection(t *testing.T) {
	const base = "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs"
	minimal := fileCleanupHTTPFixture()
	for _, item := range []struct {
		path     string
		response proto.Message
	}{
		{base + "?cleanupOnly=true", &cp.ListManagedKubernetesFileInputsResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupItems: []*cp.ManagedKubernetesFileCleanupView{minimal}}},
		{base + "/kfs_synthetic?cleanupOnly=true", &cp.GetManagedKubernetesFileInputResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: minimal}},
	} {
		recorder := &catalogRPCRecorder{response: item.response}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, item.path, ""))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"projection":"CLEANUP_ONLY"`) {
			t.Fatal("minimal cleanup read failed", w.Code)
		}
		for _, forbidden := range []string{"artifact", "SHA256", "object", "manifest", "transferFormat", "planRef", "streamRef"} {
			if strings.Contains(w.Body.String(), forbidden) {
				t.Fatal("source provenance leaked through cleanup projection")
			}
		}
	}
	for _, path := range []string{base + "?cleanupOnly=true&cleanupOnly=false", base + "?cleanupOnly=1", base + "/kfs_synthetic?cleanupIdempotencyKey=original-key", base + "?cleanupIdempotencyKey=original-key", base + "?cleanupOnly=true&actor=foreign"} {
		recorder := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path, ""))
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("ambiguous cleanup selector reached owner", w.Code)
		}
	}
	for _, response := range []*cp.GetManagedKubernetesFileInputResponse{
		{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_SOURCE, View: fileHTTPFixture()},
		{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: minimal, View: fileHTTPFixture()},
		{Projection: 99, CleanupView: minimal},
		{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: &cp.ManagedKubernetesFileCleanupView{SourceRef: "kfs_foreign00", InvocationRef: "inv_synthetic", Cleanup: minimal.Cleanup}},
	} {
		recorder := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		fileHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, base+"/kfs_synthetic?cleanupOnly=true", ""))
		if w.Code != 502 {
			t.Fatal("mixed cleanup projection disclosed", w.Code)
		}
	}
}

// TestKubernetesFileCleanupHTTPRepair exercises command mapping independently of release admission.
// TestKubernetesFileCleanupHTTPRepair проверяет преобразование команды отдельно от допуска в релизе.
func TestKubernetesFileCleanupHTTPRepair(t *testing.T) {
	const path = "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs/kfs_synthetic/cleanup-repair"
	serve := func(w http.ResponseWriter, recorder *catalogRPCRecorder, body string) {
		server := &Server{control: &controlplaneclient.Client{KubernetesFiles: cp.NewManagedKubernetesFileServiceClient(recorder)}}
		server.repairManagedKubernetesFileCleanup(w, inputHTTPRequest(http.MethodPost, path, body), "inv_synthetic", "kfs_synthetic",
			generated.RepairManagedKubernetesFileCleanupParams{IdempotencyKey: "public-input-key", IfMatch: `"3"`})
	}
	view := fileCleanupHTTPFixture()
	view.Cleanup.Version = 4
	view.Cleanup.RepairGeneration = 1
	view.Cleanup.ExhaustedCount = 0
	view.Cleanup.EligibleExhaustedCount = 0
	response := &cp.RepairManagedKubernetesFileCleanupResponse{Projection: cp.ManagedKubernetesFileProjection_MANAGED_KUBERNETES_FILE_PROJECTION_CLEANUP_ONLY, CleanupView: view, CleanupReceipt: &cp.ManagedKubernetesFileCleanupRepairReceipt{SourceRef: view.SourceRef, Version: 4, RepairGeneration: 1, RepairedCount: 1, AcceptedAt: timestamppb.Now()}}
	recorder := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	serve(w, recorder, "{}")
	if w.Code != 200 || recorder.method != cp.ManagedKubernetesFileService_RepairManagedKubernetesFileCleanup_FullMethodName || w.Header().Get("ETag") != `"4"` {
		t.Fatal("closure repair not forwarded exactly", w.Code)
	}
	request := recorder.request.(*cp.RepairManagedKubernetesFileCleanupRequest)
	if request.InvocationRef != view.InvocationRef || request.SourceRef != view.SourceRef || request.Mutation.GetIdempotencyKey() != "public-input-key" || request.Mutation.GetExpectedVersion() != 3 {
		t.Fatal("cleanup mutation coordinates changed")
	}
	for _, body := range []string{`{"artifactRef":"art_foreign"}`, `{"force":true}`, `{"sourceRef":"kfs_foreign00"}`, `{} {}`, `null`} {
		recorder := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		serve(w, recorder, body)
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("caller material or authority reached repair", w.Code)
		}
	}
	response.CleanupReceipt = nil
	recorder = &catalogRPCRecorder{response: response}
	w = httptest.NewRecorder()
	serve(w, recorder, "{}")
	if w.Code != 502 {
		t.Fatal("repair success without exact receipt")
	}
}
