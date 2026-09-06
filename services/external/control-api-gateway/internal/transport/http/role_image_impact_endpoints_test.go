package httptransport

import (
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

func roleImpactFixture() *cp.RoleImageImpactPlan {
	now := time.Unix(1000, 0)
	digest := strings.Repeat("a", 64)
	return &cp.RoleImageImpactPlan{Ref: "riip_fixture01", Version: 1, ConfigurationRef: "mcfg_fixture01", ConfigurationVersion: 3, RevisionRef: "mrev_fixture01", RevisionDigest: digest, RecipeRef: "recipe_fixture01", RecipeGeneration: 2, BuildRef: "build_fixture01", ArtifactRef: "image_fixture01", ArtifactDigest: "sha256:" + digest, AdmissionPolicyDigest: digest, Digest: digest, Total: 2, State: cp.RoleImageImpactPlanState_ROLE_IMAGE_IMPACT_PLAN_STATE_PREPARED, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
}

func TestRoleImageImpactGetRejectsInvalidPlanAndPage(t *testing.T) {
	plan := roleImpactFixture()
	client := &catalogRPCRecorder{response: &cp.GetRoleImageImpactPlanResponse{Plan: plan, Total: 2, Page: &cp.PageInfo{NextPageToken: "owner-next"}}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/role-image-impact-plans/riip_fixture01?query=Environment&pageSize=3&pageToken=owner-first", nil))
	request, ok := client.request.(*cp.GetRoleImageImpactPlanRequest)
	if w.Code != 200 || !ok || request.PlanRef != plan.Ref || request.Query != "Environment" || request.GetPage().GetPageToken() != "owner-first" || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("plan page: %d %s", w.Code, w.Body.String())
	}
	for name, mutate := range map[string]func(*cp.RoleImageImpactPlan){
		"unknown state":     func(v *cp.RoleImageImpactPlan) { v.State = 999 },
		"unsafe version":    func(v *cp.RoleImageImpactPlan) { v.Version = maximumSafeJSONInteger + 1 },
		"missing admission": func(v *cp.RoleImageImpactPlan) { v.AdmissionPolicyDigest = "" },
		"mutable image tag": func(v *cp.RoleImageImpactPlan) { v.ArtifactDigest = "latest" },
		"reversed expiry":   func(v *cp.RoleImageImpactPlan) { v.ExpiresAt = v.CreatedAt },
		"overflow":          func(v *cp.RoleImageImpactPlan) { v.Total = 1001 },
	} {
		t.Run(name, func(t *testing.T) {
			v := roleImpactFixture()
			mutate(v)
			if _, ok := roleImageImpactPlanView(v); ok {
				t.Fatal("invalid plan accepted")
			}
		})
	}
}
func TestRoleImageImpactPrepareAndRebindUseImmutablePlan(t *testing.T) {
	plan := roleImpactFixture()
	client := &catalogRPCRecorder{response: &cp.PrepareRoleImageImpactPlanResponse{Plan: plan}}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client), Query: cp.NewPlatformQueryServiceClient(client)}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest("POST", "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact-plans", ""))
	request, ok := client.request.(*cp.PrepareRoleImageImpactPlanRequest)
	if w.Code != 201 || !ok || request.GetMutation().GetExpectedVersion() != 3 || request.ConfigurationRef != plan.ConfigurationRef || request.RevisionRef != plan.RevisionRef {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	configuration, revision := managedFixture()
	configuration.Version = 4
	configuration.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE
	configuration.SourceEditable = proto.Bool(true)
	revision.SourceAvailable = proto.Bool(true)
	revision.State = cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED
	plan.Version = 2
	plan.State = cp.RoleImageImpactPlanState_ROLE_IMAGE_IMPACT_PLAN_STATE_APPLIED
	client.response = &cp.RebindRoleImageConsumersResponse{Configuration: configuration, Revision: revision, Plan: plan}
	body := `{"planRef":"riip_fixture01","impactDigest":"` + plan.Digest + `","selectedItemRefs":["riitem_fixture01"]}`
	path := "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest("POST", path, body))
	applied, ok := client.request.(*cp.RebindRoleImageConsumersRequest)
	if w.Code != 200 || !ok || applied.PlanRef != plan.Ref || len(applied.SelectedItemRefs) != 1 || len(applied.Consumers) != 0 || w.Header().Get("ETag") != `"4"` || !strings.Contains(w.Body.String(), `"plan"`) {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	for _, invalid := range []string{`{"impactDigest":"` + plan.Digest + `","consumers":[]}`, `{"planRef":"riip_fixture01","impactDigest":"` + plan.Digest + `","selectedItemRefs":["riitem_fixture01","riitem_fixture01"]}`} {
		client.method = ""
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, managedTestRequest("POST", path, invalid))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("legacy/duplicate reached owner: %d", w.Code)
		}
	}
}
func TestRoleImageImpactItemsKeepEnvironmentAndAgentOutcomes(t *testing.T) {
	source := &cp.RoleImageImpactItem{Ref: "riitem_fixture01", EnvironmentRef: "env_fixture01", EnvironmentVersion: 2, SourceVersionRef: "envv_fixture01", SourceVersionDigest: strings.Repeat("a", 64), Consumer: &cp.RuntimeEnvironmentConsumer{ProjectRef: "prj_fixture01", VersionRef: "envv_fixture01"}, Outcome: cp.RoleImageImpactOutcome_ROLE_IMAGE_IMPACT_OUTCOME_APPLIED, ResultEnvironmentVersionRef: "envv_new0001"}
	item, ok := roleImageImpactItemView(source, "APPLIED")
	if !ok || item.Consumer != nil || item.ResultEnvironmentVersionRef == nil {
		t.Fatal("environment-only outcome lost")
	}
	source.Consumer.AgentRef = "agt_fixture01"
	source.Consumer.AgentVersion = 3
	source.Consumer.BindingRef = "envb_fixture01"
	source.Consumer.BindingVersion = 4
	source.ResultBindingRef = "envb_fixture01"
	source.ResultBindingVersion = 5
	if _, ok := roleImageImpactItemView(source, "APPLIED"); !ok {
		t.Fatal("agent outcome rejected")
	}
	source.ResultBindingVersion = 4
	if _, ok := roleImageImpactItemView(source, "APPLIED"); ok {
		t.Fatal("unchanged binding result accepted")
	}
	source.Outcome = cp.RoleImageImpactOutcome_ROLE_IMAGE_IMPACT_OUTCOME_CONFLICT
	if _, ok := roleImageImpactItemView(source, "APPLIED"); ok {
		t.Fatal("conflict with effects accepted")
	}
	source.ResultBindingRef = ""
	source.ResultBindingVersion = 0
	source.ResultEnvironmentVersionRef = ""
	if _, ok := roleImageImpactItemView(source, "APPLIED"); !ok {
		t.Fatal("clean conflict rejected")
	}
	if _, ok := roleImageImpactItemView(source, "PREPARED"); ok {
		t.Fatal("terminal result in prepared plan accepted")
	}
}
