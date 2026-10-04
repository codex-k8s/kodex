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

func organizationImpactConsumer() *cp.RuntimeSecretImpactConsumer {
	v := secretPlanItemFixture().Consumer
	v.ScopeKind, v.ProjectRef = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, ""
	v.Consumer.ScopeKind, v.Consumer.ProjectRef = v.ScopeKind, ""
	return v
}

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
