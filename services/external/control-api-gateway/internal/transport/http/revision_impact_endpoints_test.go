package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const revisionImpactPublishBody = `{"planRef":"rip_fixture01","selectedItemRefs":[]}`

func revisionImpactFixture() *cp.RevisionImpactPlan {
	now := time.Unix(1000, 0)
	return &cp.RevisionImpactPlan{Ref: "rip_fixture01", Version: 1, Kind: cp.RevisionImpactKind_REVISION_IMPACT_KIND_RUNTIME_ENVIRONMENT, DraftRef: "renvd_fixture01", DraftVersion: 3, TargetDigest: strings.Repeat("a", 64), Digest: strings.Repeat("b", 64), Total: 1, State: cp.RevisionImpactState_REVISION_IMPACT_STATE_PREPARED, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
}

func TestRevisionImpactPrepareAndFilteredRead(t *testing.T) {
	plan := revisionImpactFixture()
	client := &catalogRPCRecorder{response: &cp.PrepareEnvironmentDraftImpactResponse{Plan: plan}}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client), Query: cp.NewPlatformQueryServiceClient(client)}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/impact-plans", ""))
	request, ok := client.request.(*cp.PrepareEnvironmentDraftImpactRequest)
	if w.Code != 201 || !ok || request.DraftRef != plan.DraftRef || request.GetMutation().GetExpectedVersion() != 3 {
		t.Fatalf("prepare failed: %d %s", w.Code, w.Body.String())
	}
	client.response = &cp.GetRevisionImpactPlanResponse{Plan: plan, Total: 0}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/revision-impact-plans/rip_fixture01?query=literal%25&pageSize=2", nil))
	read, ok := client.request.(*cp.GetRevisionImpactPlanRequest)
	if w.Code != 200 || !ok || read.Query != "literal%" || read.GetPage().GetPageSize() != 2 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("filtered read failed: %d %s", w.Code, w.Body.String())
	}
}

func TestRevisionImpactRejectsInventedPinsAndOutcomes(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RevisionImpactPlan){
		"partial source":        func(p *cp.RevisionImpactPlan) { p.SourceVersion = 1 },
		"unknown kind":          func(p *cp.RevisionImpactPlan) { p.Kind = 999 },
		"unknown state":         func(p *cp.RevisionImpactPlan) { p.State = 999 },
		"premature publication": func(p *cp.RevisionImpactPlan) { p.PublishedRevisionRef = "renvv_invented01" },
		"missing publication":   func(p *cp.RevisionImpactPlan) { p.State = cp.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED },
		"unsafe version":        func(p *cp.RevisionImpactPlan) { p.DraftVersion = maximumSafeJSONInteger + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			p := revisionImpactFixture()
			mutate(p)
			if _, ok := revisionImpactPlanView(p); ok {
				t.Fatal("invalid owner plan accepted")
			}
		})
	}
	p := revisionImpactFixture()
	p.State = cp.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED
	p.Version = 2
	p.PublishedRevisionRef = "renvv_published01"
	plan, _ := revisionImpactPlanView(p)
	item := &cp.RevisionImpactItem{Ref: "rii_fixture01", ProjectRef: "prj_fixture01", ConsumerKind: cp.RevisionImpactConsumerKind_REVISION_IMPACT_CONSUMER_KIND_AGENT, ConsumerRef: "agent_fixture01", ConsumerVersion: 3, BindingRef: "binding_fixture01", BindingVersion: 4, SourceRevisionRef: "renvv_previous01", Outcome: cp.RevisionImpactOutcome_REVISION_IMPACT_OUTCOME_APPLIED, ResultRevisionRef: p.PublishedRevisionRef, ResultBindingRef: "binding_fixture01", ResultBindingVersion: 5, ResultConsumerVersion: 4}
	if _, ok := revisionImpactItemView(item, plan); !ok {
		t.Fatal("valid owner outcome rejected")
	}
	item.ResultBindingRef = "binding_foreign01"
	if _, ok := revisionImpactItemView(item, plan); ok {
		t.Fatal("foreign result binding accepted")
	}
}

func TestRevisionImpactPublicationRejectsLegacyAndDuplicateSelectionBeforeOwner(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"planRef":"rip_fixture01"}`, `{"planRef":"rip_fixture01","selectedItemRefs":["rii_fixture01","rii_fixture01"]}`} {
		client := &environmentDraftRecorder{}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", body))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid publication reached owner: %d", w.Code)
		}
	}
}

func TestRevisionImpactPublicationRejectsMismatchedOwnerReceipt(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeEnvironmentDraft){
		"stale draft version":         func(d *cp.RuntimeEnvironmentDraft) { d.Version = 3 },
		"different validated content": func(d *cp.RuntimeEnvironmentDraft) { d.ValidationDigest = strings.Repeat("c", 64) },
		"different target":            func(d *cp.RuntimeEnvironmentDraft) { d.PublishedEnvironmentRef = "renv_other01" },
	} {
		t.Run(name, func(t *testing.T) {
			client := &environmentDraftRecorder{mutate: mutate}
			w := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", revisionImpactPublishBody))
			if w.Code != 502 || strings.Contains(w.Body.String(), "TYPE_Черновик") {
				t.Fatal("mismatched receipt exposed as publication")
			}
		})
	}
}
