package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// organizationSecretListRecorder captures the dedicated organization-secret list method and cloned
// request, returning a configured response or owner failure.
// organizationSecretListRecorder сохраняет отдельный метод списка секретов организации и копию
// запроса, возвращая заданный ответ либо ошибку владельца.
type organizationSecretListRecorder struct {
	grpc.ClientConnInterface
	method   string
	request  *cp.ListOrganizationRuntimeSecretsRequest
	response *cp.ListOrganizationRuntimeSecretsResponse
	failure  error
}

// Invoke records the organization-scoped typed list request and merges the synthetic response
// unless the configured owner error is returned.
// Invoke записывает типизированный запрос списка области организации и переносит синтетический
// ответ, если не задана ошибка владельца.
func (c *organizationSecretListRecorder) Invoke(_ context.Context, method string, input, output any, _ ...grpc.CallOption) error {
	c.method = method
	c.request = proto.Clone(input.(proto.Message)).(*cp.ListOrganizationRuntimeSecretsRequest)
	if c.failure != nil {
		return c.failure
	}
	proto.Merge(output.(proto.Message), c.response)
	return nil
}

// scopedSecretFixture builds synthetic active secret metadata from the shared draft fixture,
// selecting an explicit project or organization owner tuple.
// scopedSecretFixture создаёт синтетические метаданные активного секрета из общего черновика с
// явным выбором кортежа владельца проекта либо организации.
func scopedSecretFixture(organization bool) *cp.RuntimeSecret {
	draft := draftFixture()
	secret := &cp.RuntimeSecret{Ref: draft.SecretRef, ScopeKind: draft.ScopeKind, OrganizationRef: draft.OrganizationRef, ProjectRef: draft.ProjectRef, Name: draft.Name, ValueType: draft.ValueType, State: "ACTIVE", Version: 4, CurrentRevision: 2, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt}
	if organization {
		secret.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		secret.ProjectRef = ""
	}
	return secret
}

// TestOrganizationSecretDraftUsesCanonicalOwnerAndWriteOnlyGrantPath checks the dedicated
// organization prepare RPC, content digest and write-only broker grant path, buffer clearing and
// effect-free terminal replay.
// TestOrganizationSecretDraftUsesCanonicalOwnerAndWriteOnlyGrantPath проверяет отдельный prepare
// RPC организации, дайджест содержимого, путь write-only grant broker, очистку буфера и terminal
// replay без повторного эффекта.
func TestOrganizationSecretDraftUsesCanonicalOwnerAndWriteOnlyGrantPath(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "effect", true: "terminal replay"}[replay], func(t *testing.T) {
			c := &secretDraftRecorder{organization: true, replay: replay}
			w := httptest.NewRecorder()
			r := managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-secret-drafts", `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value"}`)
			r.Header.Set(boundary.ProjectReferenceHeader, "prj_untrusted01")
			secretDraftHandler(c).ServeHTTP(w, r)
			if w.Code != http.StatusCreated || c.calls[0] != cp.PlatformCommandService_PrepareOrganizationRuntimeSecretDraft_FullMethodName {
				t.Fatalf("canonical organization command not used: status=%d calls=%v body=%s", w.Code, c.calls, w.Body.String())
			}
			input, ok := c.requests[0].(*cp.PrepareOrganizationRuntimeSecretDraftRequest)
			if !ok || input.Name != "Fixture" || input.ExpectedContentSha256 != runtimeSecretSHA256(sha256.Sum256([]byte("fixture-only-value"))) || input.Mutation.GetExpectedVersion() != 0 || input.Mutation.GetIdempotencyKey() != "managed-fixture-01" {
				t.Fatalf("owner command lost content/version/idempotency binding: %v", input)
			}
			for _, field := range []string{"project_ref", "organization_ref", "actor_ref", "value"} {
				if input.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(field)) != nil {
					t.Fatalf("organization authority/plaintext became caller payload: %s", field)
				}
			}
			if replay {
				if len(c.calls) != 1 {
					t.Fatalf("terminal replay caused another effect: %v", c.calls)
				}
			} else {
				if len(c.calls) != 3 || c.calls[1] != sb.SecretBrokerService_CheckSecretDraftReadiness_FullMethodName || c.calls[2] != sb.SecretBrokerService_SaveSecretDraft_FullMethodName {
					t.Fatalf("non-atomic owner/readiness/grant chain: %v", c.calls)
				}
				grant := c.requests[2].(*sb.SaveSecretDraftRequest)
				if grant.OperationGrant != "fixture-server-grant" || string(grant.Value) != "fixture-only-value" {
					t.Fatal("broker did not receive exact owner grant and committed body")
				}
				for _, b := range c.retainedValue {
					if b != 0 {
						t.Fatal("plaintext buffer retained after response")
					}
				}
			}
			var view map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			if view["scopeKind"] != "ORGANIZATION" || view["organizationRef"] != "org_fixture01" || view["projectRef"] != "" || view["state"] != "DRAFT" {
				t.Fatalf("organization tuple lost in public DTO: %v", view)
			}
			assertScopedSecretSafeResponse(t, w)
		})
	}
}

// TestOrganizationSecretDraftRejectsCallerAuthorityAndMalformedOwnerReceipt checks caller
// authority rejection before prepare and malformed organization scope rejection before broker
// calls for both prepared and completed receipts.
// TestOrganizationSecretDraftRejectsCallerAuthorityAndMalformedOwnerReceipt проверяет отказ
// полномочиям клиента до prepare и некорректной области организации до вызовов broker для prepared
// и completed квитанций.
func TestOrganizationSecretDraftRejectsCallerAuthorityAndMalformedOwnerReceipt(t *testing.T) {
	for _, extra := range []string{`"projectRef":"prj_fixture01"`, `"organizationRef":"org_other01"`, `"scopeKind":"PROJECT"`, `"actorRef":"usr_other01"`} {
		t.Run(extra, func(t *testing.T) {
			c := &secretDraftRecorder{organization: true}
			w := httptest.NewRecorder()
			body := `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value",` + extra + `}`
			secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-secret-drafts", body))
			if w.Code != http.StatusBadRequest || len(c.calls) != 0 {
				t.Fatalf("caller authority reached owner: status=%d calls=%v", w.Code, c.calls)
			}
		})
	}
	for name, mutate := range map[string]func(*cp.RuntimeSecretDraftOperationReceipt){
		"missing scope": func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.ScopeKind = 0 },
		"unknown scope": func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.ScopeKind = 99 },
		"project instead of organization": func(o *cp.RuntimeSecretDraftOperationReceipt) {
			o.Draft.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
			o.Draft.ProjectRef = "prj_fixture01"
		},
		"organization carrying project": func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.ProjectRef = "prj_fixture01" },
		"missing organization":          func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.OrganizationRef = "" },
	} {
		for _, replay := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/prepared", true: "/completed"}[replay], func(t *testing.T) {
				c := &secretDraftRecorder{organization: true, replay: replay, mutateReceipt: mutate}
				w := httptest.NewRecorder()
				secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-secret-drafts", `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value"}`))
				if w.Code != http.StatusBadGateway || len(c.calls) != 1 {
					t.Fatalf("invalid owner scope reached broker/readback: status=%d calls=%v", w.Code, c.calls)
				}
			})
		}
	}
}

// TestOrganizationRuntimeSecretListUsesCanonicalOwnerScopeAndPagination checks the dedicated
// organization list RPC with query and cursor, ignoring an untrusted project header and retaining
// safe public owner scope.
// TestOrganizationRuntimeSecretListUsesCanonicalOwnerScopeAndPagination проверяет отдельный RPC
// списка организации с запросом и курсором, игнорированием недоверенного заголовка проекта и
// сохранением безопасной публичной области владельца.
func TestOrganizationRuntimeSecretListUsesCanonicalOwnerScopeAndPagination(t *testing.T) {
	c := &organizationSecretListRecorder{response: &cp.ListOrganizationRuntimeSecretsResponse{Secrets: []*cp.RuntimeSecret{scopedSecretFixture(true)}, Page: &cp.PageInfo{NextPageToken: "org-cursor"}}}
	h := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c)}})
	w := httptest.NewRecorder()
	r := managedTestRequest(http.MethodGet, "/api/v1/organization/runtime-secrets?query=fixture&pageSize=9&pageToken=org-bound-cursor", "")
	r.Header.Set(boundary.ProjectReferenceHeader, "prj_untrusted01")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || c.method != cp.PlatformQueryService_ListOrganizationRuntimeSecrets_FullMethodName || c.request.Query != "fixture" || c.request.Page.PageSize != 9 || c.request.Page.PageToken != "org-bound-cursor" {
		t.Fatalf("organization list mapping failed: status=%d method=%s request=%v body=%s", w.Code, c.method, c.request, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"scopeKind":"ORGANIZATION"`) || !strings.Contains(w.Body.String(), `"organizationRef":"org_fixture01"`) || !strings.Contains(w.Body.String(), `"nextPageToken":"org-cursor"`) {
		t.Fatal("organization list lost canonical scope/cursor")
	}
	assertScopedSecretSafeResponse(t, w)
}

// TestOrganizationRuntimeSecretListRejectsMalformedAndMixedOwnerScopesAtomically checks that a
// malformed or foreign second row rejects the entire organization-secret page without partially
// exposing the valid first row.
// TestOrganizationRuntimeSecretListRejectsMalformedAndMixedOwnerScopesAtomically проверяет отказ
// всей странице секретов организации при некорректной либо чужой второй строке без частичной
// выдачи корректной первой строки.
func TestOrganizationRuntimeSecretListRejectsMalformedAndMixedOwnerScopesAtomically(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ListOrganizationRuntimeSecretsResponse){
		"missing scope":        func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1].ScopeKind = 0 },
		"unknown scope":        func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1].ScopeKind = 99 },
		"project secret":       func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1] = scopedSecretFixture(false) },
		"org with project":     func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1].ProjectRef = "prj_fixture01" },
		"missing organization": func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1].OrganizationRef = "" },
		"foreign organization": func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1].OrganizationRef = "org_other01" },
		"nil item":             func(r *cp.ListOrganizationRuntimeSecretsResponse) { r.Secrets[1] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			response := &cp.ListOrganizationRuntimeSecretsResponse{Secrets: []*cp.RuntimeSecret{scopedSecretFixture(true), scopedSecretFixture(true)}}
			response.Secrets[1].Ref = "sec_fixture02"
			mutate(response)
			c := &organizationSecretListRecorder{response: response}
			w := httptest.NewRecorder()
			generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c)}}).ServeHTTP(w, managedTestRequest(http.MethodGet, "/api/v1/organization/runtime-secrets", ""))
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "sec_fixture01") || strings.Contains(w.Body.String(), "org_other01") {
				t.Fatalf("partial or foreign secret page escaped: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestOrganizationRuntimeSecretRoutesPropagateOwnerErrorsWithoutPrivateDetails checks concrete
// owner error mapping on organization list and draft creation while ensuring denied prepare never
// reaches the broker.
// TestOrganizationRuntimeSecretRoutesPropagateOwnerErrorsWithoutPrivateDetails проверяет
// конкретное отображение ошибок владельца в списке и создании черновика организации и отсутствие
// вызова broker после отказа prepare.
func TestOrganizationRuntimeSecretRoutesPropagateOwnerErrorsWithoutPrivateDetails(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unauthenticated: 401, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503} {
		t.Run(code.String(), func(t *testing.T) {
			for _, create := range []bool{false, true} {
				w := httptest.NewRecorder()
				failure := status.Error(code, "private upstream detail")
				if create {
					c := &secretDraftRecorder{organization: true, failMethod: "PrepareOrganizationRuntimeSecretDraft", failure: failure}
					secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-secret-drafts", `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value"}`))
					if len(c.calls) != 1 {
						t.Fatal("denied organization prepare reached broker")
					}
				} else {
					c := &organizationSecretListRecorder{failure: failure}
					generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c)}}).ServeHTTP(w, managedTestRequest(http.MethodGet, "/api/v1/organization/runtime-secrets", ""))
				}
				if w.Code != expected || strings.Contains(w.Body.String(), "private upstream") {
					t.Fatalf("owner error was changed or leaked: status=%d", w.Code)
				}
			}
		})
	}
}

// assertScopedSecretSafeResponse rejects fixture plaintext, grants and private storage fields in
// responses and requires no-store, no-cache and zero expiry headers.
// assertScopedSecretSafeResponse отклоняет тестовые plaintext, grants и внутренние поля хранения в
// ответах и требует заголовки no-store, no-cache и нулевого истечения.
func assertScopedSecretSafeResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, private := range []string{"fixture-only-value", "fixture-server-grant", "operationGrant", "ciphertext", "encryptionKey", "secretUid", "resourceVersion"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("private secret material escaped in response: %s", private)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" || w.Header().Get("Expires") != "0" {
		t.Fatal("secret response caching was enabled")
	}
}
