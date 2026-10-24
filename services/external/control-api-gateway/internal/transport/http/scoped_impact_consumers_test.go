package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// organizationImpactConsumer adapts the shared synthetic impact consumer to an explicit
// organization scope with no project locator in either owner tuple.
// organizationImpactConsumer переводит общего синтетического потребителя влияния в явную область
// организации без ссылки проекта в обоих кортежах владельца.
func organizationImpactConsumer() *cp.RuntimeSecretImpactConsumer {
	v := secretPlanItemFixture().Consumer
	v.ScopeKind, v.ProjectRef = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, ""
	v.Consumer.ScopeKind, v.Consumer.ProjectRef = v.ScopeKind, ""
	return v
}

// TestScopedSecretImpactRequiresFullOwnerTuple checks complete and matching owner tuples for
// impact and nested consumers, allowing an unbound environment without inferring missing scope.
// TestScopedSecretImpactRequiresFullOwnerTuple проверяет полные совпадающие кортежи владельца у
// влияния и вложенных потребителей, допуская непривязанное окружение без вывода отсутствующей
// области.
func TestScopedSecretImpactRequiresFullOwnerTuple(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecretImpactConsumer){
		"missing scope":             func(v *cp.RuntimeSecretImpactConsumer) { v.ScopeKind = 0 },
		"unknown scope":             func(v *cp.RuntimeSecretImpactConsumer) { v.ScopeKind = 99 },
		"missing organization":      func(v *cp.RuntimeSecretImpactConsumer) { v.OrganizationRef = "" },
		"organization with project": func(v *cp.RuntimeSecretImpactConsumer) { v.ProjectRef = "prj_foreign01" },
		"project without project": func(v *cp.RuntimeSecretImpactConsumer) {
			v.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
		},
		"foreign binding organization": func(v *cp.RuntimeSecretImpactConsumer) { v.Consumer.OrganizationRef = "org_foreign01" },
		"foreign binding scope": func(v *cp.RuntimeSecretImpactConsumer) {
			v.Consumer.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
		},
		"foreign binding project": func(v *cp.RuntimeSecretImpactConsumer) { v.Consumer.ProjectRef = "prj_foreign01" },
	} {
		t.Run(name, func(t *testing.T) {
			v := organizationImpactConsumer()
			mutate(v)
			if _, ok := secretImpactConsumerView(v, 0); ok {
				t.Fatal("invalid owner snapshot accepted")
			}
		})
	}
	for _, unbound := range []bool{false, true} {
		v := organizationImpactConsumer()
		if unbound {
			v.Consumer = nil
		}
		result, ok := secretImpactConsumerView(v, 0)
		if !ok || result.ScopeKind != generated.RuntimeResourceScopeKindORGANIZATION || result.OrganizationRef != "org_fixture01" || result.ProjectRef != "" || (result.Consumer == nil) != unbound {
			t.Fatal("organizational owner snapshot lost")
		}
	}
}

// TestScopedSecretRebindPreservesOrganizationAndRejectsForgedReceipts checks organization-scoped
// rebind request pins and safe owner receipt projection, rejecting an environment receipt from a
// foreign organization.
// TestScopedSecretRebindPreservesOrganizationAndRejectsForgedReceipts проверяет pins запроса
// перепривязки области организации и безопасную проекцию квитанции владельца с отказом квитанции
// окружения чужой организации.
func TestScopedSecretRebindPreservesOrganizationAndRejectsForgedReceipts(t *testing.T) {
	body := strings.ReplaceAll(strings.ReplaceAll(secretRebindBody, `"PROJECT"`, `"ORGANIZATION"`), `"prj_fixture01"`, `""`)
	for _, corrupt := range []bool{false, true} {
		client := &secretImpactRecorder{corrupt: func(message proto.Message) {
			env := message.(*cp.RebindRuntimeSecretResponse).Environments[0]
			env.ScopeKind, env.ProjectRef = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, ""
			if corrupt {
				env.OrganizationRef = "org_foreign01"
			}
		}}
		w := httptest.NewRecorder()
		secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, secretRevisionPath+"/consumer-bindings", body))
		want := http.StatusOK
		if corrupt {
			want = http.StatusBadGateway
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
		input := client.request.(*cp.RebindRuntimeSecretRequest).Selections[0]
		if input.ScopeKind != cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION || input.OrganizationRef != "org_fixture01" || input.ProjectRef != "" || input.Consumers[0].ScopeKind != input.ScopeKind {
			t.Fatal("immutable owner expectation lost")
		}
		if !corrupt && (!strings.Contains(w.Body.String(), `"scopeKind":"ORGANIZATION"`) || !strings.Contains(w.Body.String(), `"organizationRef":"org_fixture01"`)) {
			t.Fatal("receipt owner tuple lost")
		}
	}
}

// TestScopedRebindMissingTupleRejectedBeforeRPC checks missing, empty or unknown
// scope/organization fields in selections and nested consumers fail before the rebind owner RPC.
// TestScopedRebindMissingTupleRejectedBeforeRPC проверяет отказ отсутствующим, пустым либо
// неизвестным полям области и организации в выборе и вложенных потребителях до RPC перепривязки
// владельца.
func TestScopedRebindMissingTupleRejectedBeforeRPC(t *testing.T) {
	for _, body := range []string{
		strings.ReplaceAll(secretRebindBody, `"scopeKind":"PROJECT",`, ""),
		strings.ReplaceAll(secretRebindBody, `"organizationRef":"org_fixture01",`, ""),
		strings.ReplaceAll(secretRebindBody, `"organizationRef":"org_fixture01"`, `"organizationRef":""`),
		strings.ReplaceAll(secretRebindBody, `"scopeKind":"PROJECT"`, `"scopeKind":"UNKNOWN"`),
	} {
		client := &secretImpactRecorder{}
		w := httptest.NewRecorder()
		secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, secretRevisionPath+"/consumer-bindings", body))
		if w.Code != http.StatusBadRequest || client.request != nil {
			t.Fatal("incomplete owner snapshot reached RPC")
		}
	}
}

// TestEnvironmentConsumerUnknownOwnerCannotInferScope checks an explicit organization consumer is
// valid while absent scope or a project scope without its project locator is rejected.
// TestEnvironmentConsumerUnknownOwnerCannotInferScope проверяет корректность явного потребителя
// организации и отказ отсутствующей области либо области проекта без его ссылки.
func TestEnvironmentConsumerUnknownOwnerCannotInferScope(t *testing.T) {
	c := environmentConsumerView(organizationImpactConsumer().Consumer)
	if !validEnvironmentConsumer(c) {
		t.Fatal("valid organizational consumer rejected")
	}
	c.ScopeKind = ""
	if validEnvironmentConsumer(c) {
		t.Fatal("scope inferred from empty project")
	}
	c.ScopeKind = generated.RuntimeResourceScopeKindPROJECT
	if validEnvironmentConsumer(c) {
		t.Fatal("project owner accepted without project")
	}
}

// TestSecretImpactPageRejectsMixedOwnerSnapshots checks that an otherwise internally consistent
// second consumer from a foreign organization rejects the entire secret-impact page.
// TestSecretImpactPageRejectsMixedOwnerSnapshots проверяет отказ всей странице влияния секрета при
// втором внутренне согласованном потребителе из чужой организации.
func TestSecretImpactPageRejectsMixedOwnerSnapshots(t *testing.T) {
	client := &secretImpactRecorder{corrupt: func(message proto.Message) {
		v := message.(*cp.GetRuntimeSecretImpactResponse).Consumers[1]
		v.OrganizationRef = "org_foreign01"
		v.Consumer.OrganizationRef = v.OrganizationRef
	}}
	w := httptest.NewRecorder()
	secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, secretRevisionPath+"/impact", ""))
	if w.Code != http.StatusBadGateway {
		t.Fatal("mixed owner page exposed")
	}
}

// TestRevisionImpactExplicitOrganizationWithoutProject checks organization impact items need no
// project locator but reject absent, unknown or project scope when that locator is empty.
// TestRevisionImpactExplicitOrganizationWithoutProject проверяет отсутствие требования ссылки
// проекта у элементов влияния организации и отказ отсутствующей, неизвестной либо проектной
// области при пустой ссылке.
func TestRevisionImpactExplicitOrganizationWithoutProject(t *testing.T) {
	p := revisionImpactFixture()
	plan, ok := revisionImpactPlanView(p)
	if !ok {
		t.Fatal("valid environment plan rejected")
	}
	v := &cp.RevisionImpactItem{Ref: "rvit_fixture01", ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_fixture01",
		ConsumerKind: cp.RevisionImpactConsumerKind_REVISION_IMPACT_CONSUMER_KIND_AGENT, ConsumerRef: "agt_fixture01", ConsumerVersion: 2, BindingRef: "bind_fixture01", BindingVersion: 1,
		SourceRevisionRef: "renvv_previous01", Outcome: cp.RevisionImpactOutcome_REVISION_IMPACT_OUTCOME_PENDING}
	result, ok := revisionImpactItemView(v, plan)
	if !ok || result.ScopeKind != generated.RuntimeResourceScopeKindORGANIZATION || result.OrganizationRef != v.OrganizationRef {
		t.Fatal("organizational environment impact lost")
	}
	for _, kind := range []cp.RuntimeResourceScopeKind{0, 99, cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT} {
		v.ScopeKind = kind
		if _, ok := revisionImpactItemView(v, plan); ok {
			t.Fatal("scope inferred from empty project")
		}
	}
}
