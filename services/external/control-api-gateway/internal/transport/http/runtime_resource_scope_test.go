package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
)

// TestRuntimeResourceScopeClosedTuple checks the closed organization/project scope tuple,
// requiring exact valid locators and rejecting unspecified, unknown or contradictory combinations.
// TestRuntimeResourceScopeClosedTuple проверяет закрытый кортеж областей организации и проекта с
// точными корректными ссылками и отказом неопределённым, неизвестным и противоречивым комбинациям.
func TestRuntimeResourceScopeClosedTuple(t *testing.T) {
	for _, tc := range []struct {
		name, kind, organization, project string
		valid                             bool
	}{
		{"organization", "ORGANIZATION", "org_fixture01", "", true},
		{"project", "PROJECT", "org_fixture01", "prj_fixture01", true},
		{"missing scope", "", "org_fixture01", "prj_fixture01", false},
		{"unspecified scope", "UNSPECIFIED", "org_fixture01", "prj_fixture01", false},
		{"unknown scope", "PLATFORM", "org_fixture01", "", false},
		{"missing org", "PROJECT", "", "prj_fixture01", false},
		{"invalid org", "ORGANIZATION", "org/other", "", false},
		{"long org", "ORGANIZATION", strings.Repeat("a", 97), "", false},
		{"organization project locator", "ORGANIZATION", "org_fixture01", "prj_fixture01", false},
		{"project absent locator", "PROJECT", "org_fixture01", "", false},
		{"invalid project", "PROJECT", "org_fixture01", "../prj_fixture01", false},
		{"long project", "PROJECT", "org_fixture01", strings.Repeat("a", 97), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := runtimeResourceScopeKind("RUNTIME_RESOURCE_SCOPE_KIND_" + tc.kind)
			if got := validRuntimeResourceScope(kind, tc.organization, tc.project); got != tc.valid {
				t.Fatalf("closed scope tuple validation=%t, want %t", got, tc.valid)
			}
		})
	}
}

// scopedCreateBrokerStub captures the create-secret broker request and supplies configurable
// synthetic metadata for scope-boundary tests.
// scopedCreateBrokerStub сохраняет запрос broker создания секрета и выдаёт настраиваемые
// синтетические метаданные для тестов границы области.
type scopedCreateBrokerStub struct {
	sb.SecretBrokerServiceClient
	result *sb.RuntimeSecretMetadata
	input  *sb.CreateSecretRequest
}

// CreateSecret records the synthetic broker create command and returns the configured metadata
// without storing secret material.
// CreateSecret записывает синтетическую команду создания broker и возвращает заданные метаданные
// без сохранения секретного материала.
func (c *scopedCreateBrokerStub) CreateSecret(_ context.Context, input *sb.CreateSecretRequest, _ ...grpc.CallOption) (*sb.CreateSecretResponse, error) {
	c.input = input
	return &sb.CreateSecretResponse{Secret: c.result}, nil
}

// TestRuntimeSecretImmediateCreateRejectsMalformedScopeAndForeignProject checks scope and project
// binding in immediate broker results and terminal replays, preserving the grant path and
// suppressing invalid metadata.
// TestRuntimeSecretImmediateCreateRejectsMalformedScopeAndForeignProject проверяет привязку
// области и проекта в непосредственном ответе broker и terminal replay с сохранением пути grant и
// скрытием некорректных метаданных.
func TestRuntimeSecretImmediateCreateRejectsMalformedScopeAndForeignProject(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecret){
		"missing scope":        func(s *cp.RuntimeSecret) { s.ScopeKind = 0 },
		"unknown scope":        func(s *cp.RuntimeSecret) { s.ScopeKind = 99 },
		"missing organization": func(s *cp.RuntimeSecret) { s.OrganizationRef = "" },
		"foreign project":      func(s *cp.RuntimeSecret) { s.ProjectRef = "prj_other01" },
		"organization scope": func(s *cp.RuntimeSecret) {
			s.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
			s.ProjectRef = ""
		},
	} {
		for _, replay := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/effect", true: "/replay"}[replay], func(t *testing.T) {
				secret := scopedSecretFixture(false)
				mutate(secret)
				command := runtimeSecretCommandStub{create: func(*cp.PrepareCreateRuntimeSecretRequest) (*cp.PrepareCreateRuntimeSecretResponse, error) {
					op := &cp.RuntimeSecretOperationReceipt{State: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_PREPARED, OperationGrant: "fixture-server-grant"}
					if replay {
						op.State = cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED
						op.OperationGrant = ""
						op.TerminalSecret = secret
					}
					return &cp.PrepareCreateRuntimeSecretResponse{Operation: op}, nil
				}}
				broker := &scopedCreateBrokerStub{result: &sb.RuntimeSecretMetadata{ScopeKind: sb.RuntimeResourceScopeKind(secret.ScopeKind), OrganizationRef: secret.OrganizationRef, ProjectRef: secret.ProjectRef, SecretRef: secret.Ref, Name: secret.Name, ValueType: sb.RuntimeSecretValueType(secret.ValueType), Status: sb.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE, Version: secret.Version, Revision: uint64(secret.CurrentRevision), CreatedAt: secret.CreatedAt, UpdatedAt: secret.UpdatedAt}}
				s := &Server{control: &controlplaneclient.Client{Command: command}, secrets: broker}
				w := httptest.NewRecorder()
				s.CreateRuntimeSecret(w, managedTestRequest(http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-secrets", `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value"}`), "prj_fixture01", generated.CreateRuntimeSecretParams{IdempotencyKey: "managed-fixture-01"})
				if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "sec_fixture01") {
					t.Fatalf("immediate secret escaped scoped receipt check: status=%d body=%s", w.Code, w.Body.String())
				}
				if replay && broker.input != nil || !replay && (broker.input == nil || broker.input.OperationGrant != "fixture-server-grant") {
					t.Fatal("terminal/effect grant path was changed")
				}
			})
		}
	}
}

// TestRuntimeSecretDraftScopeDTOClosedAndExplicit checks explicit organization/project draft DTO
// scopes and rejects missing, unknown or contradictory owner tuples without inferring legacy
// scope.
// TestRuntimeSecretDraftScopeDTOClosedAndExplicit проверяет явные области организации и проекта
// DTO черновика и отказ отсутствующим, неизвестным либо противоречивым кортежам владельца без
// вывода старой области.
func TestRuntimeSecretDraftScopeDTOClosedAndExplicit(t *testing.T) {
	for _, organization := range []bool{false, true} {
		draft := draftFixture()
		if organization {
			draft.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
			draft.ProjectRef = ""
		}
		view, ok := runtimeSecretDraftView(draft)
		if !ok || view.ScopeKind != runtimeResourceScopeKind(draft.ScopeKind.String()) || view.OrganizationRef != draft.OrganizationRef || view.ProjectRef != draft.ProjectRef {
			t.Fatal("closed owner tuple lost in draft DTO")
		}
	}
	for name, mutate := range map[string]func(*cp.RuntimeSecretDraft){
		"legacy unspecified": func(d *cp.RuntimeSecretDraft) { d.ScopeKind = 0 },
		"unknown scope":      func(d *cp.RuntimeSecretDraft) { d.ScopeKind = 99 },
		"missing org":        func(d *cp.RuntimeSecretDraft) { d.OrganizationRef = "" },
		"org with project": func(d *cp.RuntimeSecretDraft) {
			d.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		},
		"project without locator": func(d *cp.RuntimeSecretDraft) { d.ProjectRef = "" },
	} {
		t.Run(name, func(t *testing.T) {
			draft := draftFixture()
			mutate(draft)
			if _, ok := runtimeSecretDraftView(draft); ok {
				t.Fatal("legacy/invalid scope was inferred or accepted")
			}
		})
	}
}

// TestRuntimeSecretMetadataDTOCarriesSameExplicitScopeFromOwnerAndBroker checks that owner and
// broker metadata produce identical explicit public scope tuples without protobuf enum prefixes.
// TestRuntimeSecretMetadataDTOCarriesSameExplicitScopeFromOwnerAndBroker проверяет одинаковые
// явные публичные кортежи области из метаданных владельца и broker без префиксов protobuf enum.
func TestRuntimeSecretMetadataDTOCarriesSameExplicitScopeFromOwnerAndBroker(t *testing.T) {
	for _, organization := range []bool{false, true} {
		s := scopedSecretFixture(organization)
		owner := castControlPlaneRuntimeSecret(s)
		broker := castRuntimeSecretMetadata(&sb.RuntimeSecretMetadata{ScopeKind: sb.RuntimeResourceScopeKind(s.ScopeKind), OrganizationRef: s.OrganizationRef, ProjectRef: s.ProjectRef, SecretRef: s.Ref, Name: s.Name, ValueType: sb.RuntimeSecretValueType(s.ValueType), Status: sb.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE, Version: s.Version, Revision: uint64(s.CurrentRevision), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt})
		for source, dto := range map[string]generated.RuntimeSecret{"owner": owner, "broker": broker} {
			if dto.ScopeKind != runtimeResourceScopeKind(s.ScopeKind.String()) || dto.OrganizationRef != "org_fixture01" || dto.ProjectRef != s.ProjectRef {
				t.Fatalf("%s lost exact owner scope: %+v", source, dto)
			}
			body, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "RUNTIME_RESOURCE_SCOPE_KIND_") || !strings.Contains(string(body), `"scopeKind":"`+string(dto.ScopeKind)+`"`) || !strings.Contains(string(body), `"organizationRef":"org_fixture01"`) {
				t.Fatalf("%s DTO did not expose canonical public scope: %s", source, body)
			}
		}
	}
}

// TestRuntimeSecretDraftFinishRequiresExactScopeAndOrganization checks that draft validation
// rejects malformed or foreign broker scope receipts for both owner scopes after the expected
// three-step call sequence.
// TestRuntimeSecretDraftFinishRequiresExactScopeAndOrganization проверяет отказ валидации
// черновика некорректным либо чужим квитанциям области broker для обеих областей владельца после
// ожидаемых трёх вызовов.
func TestRuntimeSecretDraftFinishRequiresExactScopeAndOrganization(t *testing.T) {
	for _, organization := range []bool{false, true} {
		for name, mutate := range map[string]func(*sb.RuntimeSecretDraftMetadata){
			"missing scope": func(d *sb.RuntimeSecretDraftMetadata) { d.ScopeKind = 0 },
			"unknown scope": func(d *sb.RuntimeSecretDraftMetadata) { d.ScopeKind = 99 },
			"missing org":   func(d *sb.RuntimeSecretDraftMetadata) { d.OrganizationRef = "" },
			"foreign org":   func(d *sb.RuntimeSecretDraftMetadata) { d.OrganizationRef = "org_other01" },
			"valid cross scope tuple": func(d *sb.RuntimeSecretDraftMetadata) {
				if d.ScopeKind == sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION {
					d.ScopeKind = sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
					d.ProjectRef = "prj_fixture01"
				} else {
					d.ScopeKind = sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
					d.ProjectRef = ""
				}
			},
		} {
			t.Run(map[bool]string{false: "project/", true: "organization/"}[organization]+name, func(t *testing.T) {
				c := &secretDraftRecorder{organization: organization, mutateDraft: mutate}
				w := httptest.NewRecorder()
				secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", ""))
				if w.Code != http.StatusBadGateway || len(c.calls) != 3 || strings.Contains(w.Body.String(), "sdft_fixture01") || strings.Contains(w.Body.String(), "org_other01") {
					t.Fatalf("cross-scope broker receipt escaped: status=%d calls=%v body=%s", w.Code, c.calls, w.Body.String())
				}
			})
		}
	}
}

// TestRuntimeSecretPublicationPinsScopeAcrossEffectAndTerminalReplay checks exact organization and
// scope binding in publication effect and terminal-replay receipts without exposing a mismatched
// secret.
// TestRuntimeSecretPublicationPinsScopeAcrossEffectAndTerminalReplay проверяет точную привязку
// организации и области в квитанциях эффекта публикации и terminal replay без раскрытия
// несовпадающего секрета.
func TestRuntimeSecretPublicationPinsScopeAcrossEffectAndTerminalReplay(t *testing.T) {
	for _, organization := range []bool{false, true} {
		for name, mutate := range map[string]func(*sb.RuntimeSecretMetadata){
			"missing scope": func(d *sb.RuntimeSecretMetadata) { d.ScopeKind = 0 },
			"unknown scope": func(d *sb.RuntimeSecretMetadata) { d.ScopeKind = 99 },
			"missing org":   func(d *sb.RuntimeSecretMetadata) { d.OrganizationRef = "" },
			"foreign org":   func(d *sb.RuntimeSecretMetadata) { d.OrganizationRef = "org_other01" },
			"valid cross scope tuple": func(d *sb.RuntimeSecretMetadata) {
				if d.ScopeKind == sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION {
					d.ScopeKind = sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
					d.ProjectRef = "prj_fixture01"
				} else {
					d.ScopeKind = sb.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
					d.ProjectRef = ""
				}
			},
		} {
			for _, replay := range []bool{false, true} {
				t.Run(map[bool]string{false: "project/", true: "organization/"}[organization]+name+map[bool]string{false: "/effect", true: "/replay"}[replay], func(t *testing.T) {
					c := &secretDraftRecorder{organization: organization, replay: replay}
					if replay {
						c.mutateReceipt = func(o *cp.RuntimeSecretDraftOperationReceipt) {
							s := &sb.RuntimeSecretMetadata{ScopeKind: sb.RuntimeResourceScopeKind(o.TerminalSecret.ScopeKind), OrganizationRef: o.TerminalSecret.OrganizationRef, ProjectRef: o.TerminalSecret.ProjectRef}
							mutate(s)
							o.TerminalSecret.ScopeKind = cp.RuntimeResourceScopeKind(s.ScopeKind)
							o.TerminalSecret.OrganizationRef = s.OrganizationRef
							o.TerminalSecret.ProjectRef = s.ProjectRef
						}
					} else {
						c.mutateSecret = mutate
					}
					w := httptest.NewRecorder()
					secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/runtime-secret-drafts/sdft_fixture01/publish", `{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":["sdit_fixture01"]}`))
					expectedCalls := 3
					if replay {
						expectedCalls = 1
					}
					if w.Code != http.StatusBadGateway || len(c.calls) != expectedCalls || strings.Contains(w.Body.String(), "sec_fixture01") {
						t.Fatalf("publication crossed owner scope: status=%d calls=%v body=%s", w.Code, c.calls, w.Body.String())
					}
				})
			}
		}
	}
}

// TestProjectRuntimeSecretPageRejectsForeignScopeAndOwnerAtomically checks that a second malformed
// or foreign secret rejects the entire project page without partially returning the first valid
// secret.
// TestProjectRuntimeSecretPageRejectsForeignScopeAndOwnerAtomically проверяет отказ всей странице
// проекта при втором некорректном либо чужом секрете без частичной выдачи первого корректного
// секрета.
func TestProjectRuntimeSecretPageRejectsForeignScopeAndOwnerAtomically(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecret){
		"missing scope":   func(s *cp.RuntimeSecret) { s.ScopeKind = 0 },
		"unknown scope":   func(s *cp.RuntimeSecret) { s.ScopeKind = 99 },
		"missing org":     func(s *cp.RuntimeSecret) { s.OrganizationRef = "" },
		"foreign org":     func(s *cp.RuntimeSecret) { s.OrganizationRef = "org_other01" },
		"foreign project": func(s *cp.RuntimeSecret) { s.ProjectRef = "prj_other01" },
		"organization secret": func(s *cp.RuntimeSecret) {
			s.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
			s.ProjectRef = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			first, second := scopedSecretFixture(false), scopedSecretFixture(false)
			second.Ref = "sec_fixture02"
			mutate(second)
			query := &runtimeSecretQueryStub{list: &cp.ListRuntimeSecretsResponse{Secrets: []*cp.RuntimeSecret{first, second}}}
			s := &Server{control: &controlplaneclient.Client{Query: query}}
			w := httptest.NewRecorder()
			s.ListRuntimeSecrets(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_fixture01/runtime-secrets", nil), "prj_fixture01", generated.ListRuntimeSecretsParams{})
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "sec_fixture01") || strings.Contains(w.Body.String(), "org_other01") {
				t.Fatalf("project page partially exposed foreign scope: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestRuntimeSecretGetRejectsMissingOrMalformedScope checks that single-secret readback rejects
// missing, unknown or contradictory scopes and mismatched references without exposing metadata.
// TestRuntimeSecretGetRejectsMissingOrMalformedScope проверяет отказ чтения одного секрета
// отсутствующим, неизвестным либо противоречивым областям и несовпадающим ссылкам без выдачи
// метаданных.
func TestRuntimeSecretGetRejectsMissingOrMalformedScope(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecret){
		"missing scope": func(s *cp.RuntimeSecret) { s.ScopeKind = 0 },
		"unknown scope": func(s *cp.RuntimeSecret) { s.ScopeKind = 99 },
		"missing org":   func(s *cp.RuntimeSecret) { s.OrganizationRef = "" },
		"organization carrying project": func(s *cp.RuntimeSecret) {
			s.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		},
		"project without locator": func(s *cp.RuntimeSecret) { s.ProjectRef = "" },
		"wrong ref":               func(s *cp.RuntimeSecret) { s.Ref = "sec_other01" },
	} {
		t.Run(name, func(t *testing.T) {
			secret := scopedSecretFixture(false)
			mutate(secret)
			s := &Server{control: &controlplaneclient.Client{Query: &runtimeSecretQueryStub{get: &cp.GetRuntimeSecretResponse{Secret: secret}}}}
			w := httptest.NewRecorder()
			s.GetRuntimeSecret(w, httptest.NewRequest(http.MethodGet, "/api/v1/runtime-secrets/sec_fixture01", nil), "sec_fixture01")
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "sec_fixture01") || strings.Contains(w.Body.String(), "sec_other01") {
				t.Fatalf("malformed owner tuple/ref escaped get: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
