package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/getkin/kin-openapi/openapi3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// projectPurgeRecoveryPath names the synthetic project purge-recovery route shared by local
// preview, resolution and rejection tests.
// projectPurgeRecoveryPath задаёт маршрут восстановления очистки синтетического проекта для
// локальных тестов предпросмотра, решения и отказов.
const projectPurgeRecoveryPath = "/api/v1/projects/prj_fixture01/purge-recovery"

// TestProjectPurgeRecoveryOpenAPIContract loads the canonical OpenAPI and validates purge-recovery
// GET/POST operation IDs and their distinct preview and accepted statuses.
// TestProjectPurgeRecoveryOpenAPIContract загружает канонический OpenAPI и проверяет operation ID
// GET/POST восстановления очистки и разные статусы предпросмотра и принятого решения.
func TestProjectPurgeRecoveryOpenAPIContract(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../../../../../contracts/openapi/control-api-gateway/v1/openapi.yaml")
	if err != nil {
		t.Fatal("OpenAPI source cannot be loaded")
	}
	route := spec.Paths.Find("/api/v1/projects/{projectRef}/purge-recovery")
	if route == nil || route.Get == nil || route.Post == nil || route.Get.OperationID != "getProjectPurgeRecovery" || route.Post.OperationID != "resolveProjectPurgeRecovery" {
		t.Fatal("recovery route is missing from the generated contract")
	}
	if err := route.Validate(t.Context()); err != nil {
		t.Fatalf("recovery OpenAPI validation failed: %v", err)
	}
	if route.Get.Responses.Status(200) == nil || route.Post.Responses.Status(202) == nil {
		t.Fatal("recovery contract lost preview or accepted response")
	}
}

// projectPurgeRecoveryFixture builds a synthetic WAITING_OWNER recovery snapshot with unresolved
// cleanup counts, its own version and an independent project version.
// projectPurgeRecoveryFixture создаёт синтетический snapshot восстановления WAITING_OWNER с
// незавершёнными счётчиками очистки, собственной версией и независимой версией проекта.
func projectPurgeRecoveryFixture() *cp.ProjectPurgeRecovery {
	return &cp.ProjectPurgeRecovery{ProjectRef: "prj_fixture01", Version: 9, SnapshotDigest: strings.Repeat("a", 64),
		State: "WAITING_OWNER", UnknownContentCount: 1, CleanupPendingCount: 2, ProjectPresent: true, CanResolve: true, ProjectVersion: 13}
}

// projectPurgeResolutionBody encodes the synthetic snapshot digest and explicit unknown-outcome
// acknowledgement for local recovery resolution requests.
// projectPurgeResolutionBody кодирует дайджест синтетического snapshot и явное подтверждение
// неизвестного результата для локальных запросов решения восстановления.
func projectPurgeResolutionBody() string {
	return `{"snapshotDigest":"` + strings.Repeat("a", 64) + `","acknowledgeUnknownOutcome":true}`
}

// TestProjectPurgeRecoveryTypedRoutes checks separate preview and resolution RPCs, exact owner
// readback, snapshot acknowledgement, mutation version, idempotency, ETag and no-store.
// TestProjectPurgeRecoveryTypedRoutes проверяет отдельные RPC предпросмотра и решения, точное
// чтение владельца, подтверждение snapshot, версию изменения, идемпотентность, ETag и no-store.
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

// TestProjectPurgeRecoveryReadSurvivesProjectRemoval checks that an observing recovery receipt
// remains readable after project removal, explicitly returning absence and project version zero.
// TestProjectPurgeRecoveryReadSurvivesProjectRemoval проверяет доступность чтения наблюдаемой
// квитанции восстановления после удаления проекта с явным отсутствием проекта и нулевой версией.
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

// TestProjectPurgeRecoveryRejectsUnconfirmedOrMalformedInput checks explicit acknowledgement and
// digest syntax, unknown or trailing input and required version, idempotency and CSRF headers
// before RPC.
// TestProjectPurgeRecoveryRejectsUnconfirmedOrMalformedInput проверяет явное подтверждение и
// синтаксис дайджеста, неизвестный либо лишний ввод и обязательные заголовки версии,
// идемпотентности и CSRF до RPC.
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

// TestProjectPurgeRecoveryRejectsInvalidRecoveryETag checks rejection of wildcard, weak, zero,
// negative, unsafe and unquoted recovery versions before owner resolution.
// TestProjectPurgeRecoveryRejectsInvalidRecoveryETag проверяет отказ wildcard, слабым, нулевым,
// отрицательным, небезопасным и некавыченным версиям восстановления до решения владельца.
func TestProjectPurgeRecoveryRejectsInvalidRecoveryETag(t *testing.T) {
	for _, etag := range []string{`*`, `W/"3"`, `"0"`, `"-1"`, `"9007199254740992"`, `3`} {
		client := &catalogRPCRecorder{}
		r := managedTestRequest(http.MethodPost, projectPurgeRecoveryPath, projectPurgeResolutionBody())
		r.Header.Set("If-Match", etag)
		w := httptest.NewRecorder()
		projectTrashHandler(client).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || client.method != "" {
			t.Fatalf("invalid recovery ETag reached owner: status=%d", w.Code)
		}
	}
}

// TestProjectPurgeRecoveryPreservesOwnerRejections checks concrete HTTP mapping of authoritative
// recovery failures without disclosing the synthetic private storage descriptor.
// TestProjectPurgeRecoveryPreservesOwnerRejections проверяет конкретное HTTP-отображение
// авторитетных отказов восстановления без раскрытия синтетического внутреннего описателя
// хранилища.
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

// TestProjectPurgeRecoveryRejectsInvalidOwnerProjection checks malformed scope, versions, digest,
// state, counters and contradictory resolution availability return bad gateway without an ETag.
// TestProjectPurgeRecoveryRejectsInvalidOwnerProjection проверяет ответ bad gateway без ETag при
// повреждённых области, версиях, дайджесте, состоянии, счётчиках и противоречивой доступности
// решения.
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
		"empty resolution target":     func(v *cp.ProjectPurgeRecovery) { v.UnknownContentCount, v.CleanupPendingCount = 0, 0 },
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
