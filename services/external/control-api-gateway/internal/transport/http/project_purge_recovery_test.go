package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const projectPurgeRecoveryPath = "/api/v1/projects/prj_fixture01/purge-recovery"

func projectPurgeRecoveryFixture() *cp.ProjectPurgeRecovery {
	return &cp.ProjectPurgeRecovery{ProjectRef: "prj_fixture01", Version: 9, SnapshotDigest: strings.Repeat("a", 64),
		State: "WAITING_OWNER", UnknownContentCount: 1, CleanupPendingCount: 2, ProjectPresent: true, CanResolve: true, ProjectVersion: 13}
}

func projectPurgeResolutionBody() string {
	return `{"snapshotDigest":"` + strings.Repeat("a", 64) + `","acknowledgeUnknownOutcome":true}`
}

func TestProjectPurgeRecoveryTypedRoutes(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			value := projectPurgeRecoveryFixture()
			client := &catalogRPCRecorder{}
			wantStatus, rpc := http.StatusOK, "/GetProjectPurgeRecovery"
			client.response = &cp.GetProjectPurgeRecoveryResponse{Recovery: value}
			if method == http.MethodPost {
				value.State, value.CanResolve, value.ResolutionRef = "QUEUED", false, "prr_fixture01"
				client.response = &cp.ResolveProjectPurgeRecoveryResponse{Recovery: value}
				wantStatus, rpc = http.StatusAccepted, "/ResolveProjectPurgeRecovery"
			}
			w := httptest.NewRecorder()
			projectTrashHandler(client).ServeHTTP(w, managedTestRequest(method, projectPurgeRecoveryPath, projectPurgeResolutionBody()))
			if w.Code != wantStatus || !strings.HasSuffix(client.method, rpc) || w.Header().Get("ETag") != `"9"` || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("route status=%d rpc=%s etag=%s", w.Code, client.method, w.Header().Get("ETag"))
			}
			var view generated.ProjectPurgeRecovery
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.ProjectRef != value.ProjectRef || view.Version != 9 || view.ProjectVersion != 13 || view.ResolutionRef != value.ResolutionRef {
				t.Fatal("recovery response lost exact owner fields")
			}
			if method == http.MethodPost {
				request := client.request.(*cp.ResolveProjectPurgeRecoveryRequest)
				if request.GetProjectRef() != value.ProjectRef || request.GetSnapshotDigest() != value.SnapshotDigest || !request.GetAcknowledgeUnknownOutcome() || request.GetMutation().GetExpectedVersion() != 3 || request.GetMutation().GetIdempotencyKey() != "managed-fixture-01" {
					t.Fatal("resolution lost explicit acknowledgement or OCC/idempotency pins")
				}
			} else if client.request.(*cp.GetProjectPurgeRecoveryRequest).GetProjectRef() != value.ProjectRef {
				t.Fatal("preview changed the project locator")
			}
		})
	}
}

func TestProjectPurgeRecoveryReadSurvivesProjectRemoval(t *testing.T) {
	value := projectPurgeRecoveryFixture()
	value.ProjectPresent, value.ProjectVersion, value.CanResolve, value.State = false, 0, false, "OBSERVING"
	value.ResolutionRef = "prr_fixture01"
	client := &catalogRPCRecorder{response: &cp.GetProjectPurgeRecoveryResponse{Recovery: value}}
	w := httptest.NewRecorder()
	projectTrashHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, projectPurgeRecoveryPath, ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"projectPresent":false`) || !strings.Contains(w.Body.String(), `"projectVersion":0`) {
		t.Fatalf("surviving receipt was lost: status=%d", w.Code)
	}
}

func TestProjectPurgeRecoveryRejectsUnconfirmedOrMalformedInput(t *testing.T) {
	for _, body := range []string{"", `null`, `{}`, `{"snapshotDigest":"` + strings.Repeat("a", 64) + `"}`,
		`{"snapshotDigest":"` + strings.Repeat("a", 64) + `","acknowledgeUnknownOutcome":false}`,
		`{"snapshotDigest":"wrong","acknowledgeUnknownOutcome":true}`,
		`{"snapshotDigest":"` + strings.Repeat("A", 64) + `","acknowledgeUnknownOutcome":true}`,
		projectPurgeResolutionBody() + `{}`, strings.TrimSuffix(projectPurgeResolutionBody(), "}") + `,"actor":"owner"}`,
	} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		projectTrashHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, projectPurgeRecoveryPath, body))
		if w.Code != http.StatusBadRequest || client.method != "" {
			t.Fatalf("invalid acknowledgement reached owner: status=%d", w.Code)
		}
	}
	for _, header := range []string{"If-Match", "Idempotency-Key", "X-CSRF-Token"} {
		client := &catalogRPCRecorder{}
		r := managedTestRequest(http.MethodPost, projectPurgeRecoveryPath, projectPurgeResolutionBody())
		r.Header.Del(header)
		w := httptest.NewRecorder()
		projectTrashHandler(client).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || client.method != "" {
			t.Fatalf("missing %s reached owner: status=%d", header, w.Code)
		}
	}
}

func TestProjectPurgeRecoveryPreservesOwnerRejections(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.NotFound: 404, codes.PermissionDenied: 403, codes.Aborted: 412, codes.AlreadyExists: 409, codes.FailedPrecondition: 409, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "synthetic private storage descriptor")}
		w := httptest.NewRecorder()
		projectTrashHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, projectPurgeRecoveryPath, projectPurgeResolutionBody()))
		if w.Code != expected || strings.Contains(w.Body.String(), "private storage") {
			t.Fatalf("owner rejection changed: code=%s status=%d", code, w.Code)
		}
	}
}

func TestProjectPurgeRecoveryRejectsInvalidOwnerProjection(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ProjectPurgeRecovery){
		"wrong project":               func(v *cp.ProjectPurgeRecovery) { v.ProjectRef = "prj_other001" },
		"zero version":                func(v *cp.ProjectPurgeRecovery) { v.Version = 0 },
		"unsafe version":              func(v *cp.ProjectPurgeRecovery) { v.Version = maximumSafeJSONInteger + 1 },
		"bad digest":                  func(v *cp.ProjectPurgeRecovery) { v.SnapshotDigest = "bad" },
		"unknown state":               func(v *cp.ProjectPurgeRecovery) { v.State = "CLEAN" },
		"negative unknown":            func(v *cp.ProjectPurgeRecovery) { v.UnknownContentCount = -1 },
		"negative pending":            func(v *cp.ProjectPurgeRecovery) { v.CleanupPendingCount = -1 },
		"missing project version":     func(v *cp.ProjectPurgeRecovery) { v.ProjectVersion = 0 },
		"absent project with version": func(v *cp.ProjectPurgeRecovery) { v.ProjectPresent = false },
		"unsafe project version":      func(v *cp.ProjectPurgeRecovery) { v.ProjectVersion = maximumSafeJSONInteger + 1 },
		"unexpected resolution":       func(v *cp.ProjectPurgeRecovery) { v.ResolutionRef = "object/private/path" },
		"action inconsistent":         func(v *cp.ProjectPurgeRecovery) { v.State = "OBSERVING" },
	} {
		t.Run(name, func(t *testing.T) {
			value := projectPurgeRecoveryFixture()
			mutate(value)
			client := &catalogRPCRecorder{response: &cp.GetProjectPurgeRecoveryResponse{Recovery: value}}
			w := httptest.NewRecorder()
			projectTrashHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, projectPurgeRecoveryPath, ""))
			if w.Code != http.StatusBadGateway || w.Header().Get("ETag") != "" {
				t.Fatalf("invalid owner projection escaped: status=%d", w.Code)
			}
		})
	}
}
