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

// captureHTTPFixture represents metadata before first publication without fabricating task authority.
// captureHTTPFixture задаёт metadata до первой publication без выдуманной task authority.
func captureHTTPFixture() *cp.ManagedKubernetesCaptureStatus {
	return &cp.ManagedKubernetesCaptureStatus{Summary: &cp.ManagedKubernetesCaptureSummary{CaptureRef: "koc_synthetic", StreamRef: "kst_synthetic", CaptureGeneration: 1, Version: 3, State: cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_WAITING_OWNER, ReasonCode: "CAPTURE_RETRY_EXHAUSTED"}, InvocationRef: "inv_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", Operation: "kubernetes.exec", AttemptCount: 3, SealedAt: timestamppb.New(time.Now().Add(-time.Minute))}
}

// captureHTTPHandler uses the real generated public routes and typed client instead of a manual path alias.
// captureHTTPHandler использует real generated public routes и typed client, не ручной path alias.
func captureHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesCaptures: cp.NewManagedKubernetesCaptureServiceClient(recorder)}})
}

// TestKubernetesCapturePublicProjectionHistory validates the real list route and refuses metadata from another node.
// TestKubernetesCapturePublicProjectionHistory проверяет real list route и отклоняет metadata другого узла.
func TestKubernetesCapturePublicProjectionHistory(t *testing.T) {
	const path = "/api/v1/runs/run_synthetic/nodes/nod_synthetic/kubernetes-captures?pageSize=1"
	view := captureHTTPFixture()
	for _, foreign := range []bool{false, true} {
		item := proto.Clone(view).(*cp.ManagedKubernetesCaptureStatus)
		if foreign {
			item.NodeRef = "nod_foreignxx"
		}
		recorder := &catalogRPCRecorder{response: &cp.ListManagedKubernetesCapturesResponse{Items: []*cp.ManagedKubernetesCaptureStatus{item}, Page: &cp.PageInfo{}}}
		w := httptest.NewRecorder()
		captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path, ""))
		want := 200
		if foreign {
			want = 502
		}
		request, ok := recorder.request.(*cp.ListManagedKubernetesCapturesRequest)
		if w.Code != want || !ok || request.RunRef != "run_synthetic" || request.NodeRef != "nod_synthetic" || request.Page.PageSize != 1 || recorder.method != cp.ManagedKubernetesCaptureService_ListManagedKubernetesCaptures_FullMethodName {
			t.Fatal("capture history route crossed scope or method", w.Code)
		}
	}
	recorder := &catalogRPCRecorder{response: &cp.ListManagedKubernetesCapturesResponse{Items: []*cp.ManagedKubernetesCaptureStatus{view, view}, Page: &cp.PageInfo{}}}
	w := httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path, ""))
	if w.Code != 502 {
		t.Fatal("oversized duplicate history page disclosed", w.Code)
	}
}

// TestKubernetesCapturePublicProjectionActualRoutes checks exact repair OCC, original-key readback and unknown payload refusal.
// It proves transport mapping only; original-root persistence and permissions belong to the owner component.
// TestKubernetesCapturePublicProjectionActualRoutes проверяет exact repair OCC, original-key readback и отказ unknown payload.
// Проверяется transport mapping; original-root persistence и permissions проверяет owner component.
func TestKubernetesCapturePublicProjectionActualRoutes(t *testing.T) {
	const path = "/api/v1/kubernetes-captures/koc_synthetic"
	view := captureHTTPFixture()
	recorder := &catalogRPCRecorder{response: &cp.GetManagedKubernetesCaptureStatusResponse{View: view}}
	w := httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path+"?repairIdempotencyKey=original-repair-key", ""))
	request, ok := recorder.request.(*cp.GetManagedKubernetesCaptureStatusRequest)
	if w.Code != 200 || !ok || request.CaptureRef != "koc_synthetic" || request.RepairIdempotencyKey != "original-repair-key" || !strings.Contains(w.Body.String(), `"artifacts":[]`) {
		t.Fatal("capture original-key route lost exact metadata", w.Code)
	}
	view.Summary.State = cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_READY
	view.Summary.Version = 4
	view.Summary.ReasonCode = ""
	view.RepairGeneration = 1
	receipt := &cp.ManagedKubernetesCaptureRepairReceipt{CaptureRef: "koc_synthetic", RepairGeneration: 1, Version: 4, AttemptCount: 3, AcceptedAt: timestamppb.Now()}
	recorder = &catalogRPCRecorder{response: &cp.RepairManagedKubernetesCaptureResponse{View: view, RepairReceipt: receipt}}
	w = httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path+"/repair", "{}"))
	mutation, ok := recorder.request.(*cp.RepairManagedKubernetesCaptureRequest)
	if w.Code != 200 || !ok || mutation.Mutation.GetExpectedVersion() != 3 || mutation.Mutation.IdempotencyKey != "public-input-key" || recorder.method != cp.ManagedKubernetesCaptureService_RepairManagedKubernetesCapture_FullMethodName {
		t.Fatal("capture repair lost owner mutation", w.Code)
	}
	for _, body := range []string{`{"fence":"fake"}`, `{"outcome":"SUCCESS"}`, `{"actorRef":"usr_foreign"}`, `{"attemptCount":0}`, "[]", "null"} {
		recorder := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path+"/repair", body))
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("foreign repair fields reached owner", w.Code)
		}
	}
}

// TestKubernetesCapturePublicProjectionClosedMetadata rejects unknown/mixed upstream metadata before disclosure.
// TestKubernetesCapturePublicProjectionClosedMetadata отклоняет unknown/mixed upstream metadata до disclosure.
func TestKubernetesCapturePublicProjectionClosedMetadata(t *testing.T) {
	for _, change := range []func(*cp.ManagedKubernetesCaptureStatus){func(v *cp.ManagedKubernetesCaptureStatus) { v.Summary.State = 99 }, func(v *cp.ManagedKubernetesCaptureStatus) {
		v.Kind = cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_FULL
	}, func(v *cp.ManagedKubernetesCaptureStatus) { v.Summary.Version = 1 << 53 }, func(v *cp.ManagedKubernetesCaptureStatus) { v.SealedAt = nil }} {
		view := proto.Clone(captureHTTPFixture()).(*cp.ManagedKubernetesCaptureStatus)
		change(view)
		if _, valid := kubernetesCapturePublicReadback(view, nil); valid {
			t.Fatal("mixed capture metadata disclosed")
		}
	}
}

// TestKubernetesStagingCleanupActualRoutes preserves cleanup-specific OCC and original-key metadata.
// A cleanup ACK never means deletion, publication or quota release; unsolicited receipts are refused.
// TestKubernetesStagingCleanupActualRoutes сохраняет cleanup-specific OCC и original-key metadata.
// Cleanup ACK не означает deletion, publication либо quota release; unsolicited receipts отклоняются.
func TestKubernetesStagingCleanupActualRoutes(t *testing.T) {
	const path = "/api/v1/kubernetes-captures/koc_synthetic"
	view := captureHTTPFixture()
	view.Cleanup = &cp.ManagedKubernetesStagingCleanupStatus{Version: 4, RepairGeneration: 1, OutstandingCount: 2, OutstandingBytes: 30}
	receipt := &cp.ManagedKubernetesStagingCleanupRepairReceipt{CaptureRef: "koc_synthetic", Version: 4, RepairGeneration: 1, RepairedCount: 1, AcceptedAt: timestamppb.Now()}
	recorder := &catalogRPCRecorder{response: &cp.RepairManagedKubernetesStagingCleanupResponse{View: view, CleanupReceipt: receipt}}
	w := httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path+"/cleanup-repair", "{}"))
	request, ok := recorder.request.(*cp.RepairManagedKubernetesStagingCleanupRequest)
	if w.Code != 200 || !ok || request.Mutation.GetExpectedVersion() != 3 || request.Mutation.IdempotencyKey != "public-input-key" || recorder.method != cp.ManagedKubernetesCaptureService_RepairManagedKubernetesStagingCleanup_FullMethodName || w.Header().Get("ETag") != `"4"` {
		t.Fatal("cleanup repair lost exact purpose or OCC", w.Code)
	}
	recorder = &catalogRPCRecorder{response: &cp.GetManagedKubernetesCaptureStatusResponse{View: view, CleanupReceipt: receipt}}
	w = httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path+"?cleanupIdempotencyKey=original-cleanup-key", ""))
	read, ok := recorder.request.(*cp.GetManagedKubernetesCaptureStatusRequest)
	if w.Code != 200 || !ok || read.CleanupIdempotencyKey != "original-cleanup-key" || read.RepairIdempotencyKey != "" {
		t.Fatal("cleanup readback changed original key", w.Code)
	}
	recorder = &catalogRPCRecorder{response: &cp.GetManagedKubernetesCaptureStatusResponse{View: view, CleanupReceipt: receipt}}
	w = httptest.NewRecorder()
	captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, path, ""))
	if w.Code != 502 {
		t.Fatal("unsolicited cleanup acknowledgement disclosed", w.Code)
	}
	for _, body := range []string{`{"objectVersion":"foreign"}`, `{"attemptCount":0}`, `{"outcome":"SUCCESS"}`} {
		recorder = &catalogRPCRecorder{}
		w = httptest.NewRecorder()
		captureHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path+"/cleanup-repair", body))
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("caller cleanup state reached owner", w.Code)
		}
	}
}
