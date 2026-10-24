package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// instructionImpactFixture adapts the shared revision impact fixture to agent instructions with
// exact source version, previous revision and draft pins.
// instructionImpactFixture адаптирует общий эталон влияния ревизии к инструкциям агента с точной
// версией источника и pins предыдущей ревизии и черновика.
func instructionImpactFixture() *cp.RevisionImpactPlan {
	p := revisionImpactFixture()
	p.Kind, p.SourceRef, p.SourceVersion = cp.RevisionImpactKind_REVISION_IMPACT_KIND_AGENT_INSTRUCTIONS, "agt_fixture01", 3
	p.SourceRevisionRef, p.DraftRef = "ins_previous01", "ins_fixture01"
	return p
}

// TestInstructionsImpactPreparePublishExactReceipt checks typed prepare and publish calls, plan
// selection and agent ETag, rejecting a publication receipt with a mismatched source OCC version.
// TestInstructionsImpactPreparePublishExactReceipt проверяет типизированные вызовы prepare и
// publish, выбор плана и ETag агента с отказом квитанции публикации при несовпадении OCC версии
// источника.
func TestInstructionsImpactPreparePublishExactReceipt(t *testing.T) {
	p := instructionImpactFixture()
	client := &catalogRPCRecorder{response: &cp.PrepareInstructionsImpactResponse{Plan: p}}
	h := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/agents/agt_fixture01/instructions/impact-plans", ""))
	req, ok := client.request.(*cp.PrepareInstructionsImpactRequest)
	if w.Code != 200 || !ok || req.AgentRef != p.SourceRef || req.GetMutation().GetExpectedVersion() != 3 {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	p.State, p.Version, p.PublishedRevisionRef = cp.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED, 2, p.DraftRef
	client.response = &cp.PublishInstructionDraftResponse{Agent: &cp.Agent{Ref: p.SourceRef, ProjectRef: "prj_fixture01", Version: 4}, Plan: p}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/agents/agt_fixture01/instruction-commands", `{"action":"PUBLISH","planRef":"rip_fixture01","selectedItemRefs":[]}`))
	publication, ok := client.request.(*cp.PublishInstructionDraftRequest)
	if w.Code != 200 || !ok || publication.PlanRef != p.Ref || !strings.Contains(w.Body.String(), `"plan":`) || w.Header().Get("ETag") != `"4"` {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	p.SourceVersion = 2
	w = httptest.NewRecorder()
	h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/agents/agt_fixture01/instruction-commands", `{"action":"PUBLISH","planRef":"rip_fixture01","selectedItemRefs":[]}`))
	if w.Code != 502 {
		t.Fatalf("foreign OCC receipt: %d", w.Code)
	}
}

// TestInstructionsImpactRejectsLegacyBeforeRPC checks that missing publication plan or selection,
// repeated selected items and impact fields on VALIDATE fail before owner RPC.
// TestInstructionsImpactRejectsLegacyBeforeRPC проверяет отказ до RPC владельца при отсутствии
// плана либо выбора публикации, повторе выбранных элементов и полях влияния в VALIDATE.
func TestInstructionsImpactRejectsLegacyBeforeRPC(t *testing.T) {
	for _, body := range []string{`{"action":"PUBLISH"}`, `{"action":"PUBLISH","planRef":"rip_fixture01"}`, `{"action":"PUBLISH","planRef":"rip_fixture01","selectedItemRefs":["item_fixture01","item_fixture01"]}`, `{"action":"VALIDATE","planRef":"rip_fixture01","selectedItemRefs":[]}`} {
		client := &catalogRPCRecorder{}
		h := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/agents/agt_fixture01/instruction-commands", body))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("legacy request reached owner: %d", w.Code)
		}
	}
}

// TestPromptImpactPreparePinsExactRevision checks exact managed configuration, draft revision and
// mutation version in prompt impact preparation, rejecting a foreign prepared revision.
// TestPromptImpactPreparePinsExactRevision проверяет точные конфигурацию, ревизию черновика и
// версию изменения при подготовке влияния prompt с отказом чужой подготовленной ревизии.
func TestPromptImpactPreparePinsExactRevision(t *testing.T) {
	p := instructionImpactFixture()
	p.Kind, p.SourceRef, p.SourceRevisionRef, p.DraftRef = cp.RevisionImpactKind_REVISION_IMPACT_KIND_PROMPT_TEMPLATE, "mcfg_fixture01", "", "mrev_fixture01"
	client := &catalogRPCRecorder{response: &cp.PreparePromptTemplateImpactResponse{Plan: p}}
	h := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact-plans", ""))
	req, ok := client.request.(*cp.PreparePromptTemplateImpactRequest)
	if w.Code != 200 || !ok || req.ConfigurationRef != p.SourceRef || req.RevisionRef != p.DraftRef || req.GetMutation().GetExpectedVersion() != 3 {
		t.Fatalf("prompt prepare: %d %s", w.Code, w.Body.String())
	}
	p.DraftRef = "mrev_foreign01"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, managedTestRequest("POST", "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact-plans", ""))
	if w.Code != 502 {
		t.Fatal("foreign prepared revision accepted")
	}
}

// TestPromptImpactSupportsUnchangedEntityAndGlobalAgent checks first publication and
// organization-scoped continuation binding updates may preserve consumer version while requiring
// an advanced binding version and known consumer kind.
// TestPromptImpactSupportsUnchangedEntityAndGlobalAgent проверяет первую публикацию и изменения
// привязки продолжения области организации с неизменной версией потребителя, требуя роста версии
// привязки и известного вида потребителя.
func TestPromptImpactSupportsUnchangedEntityAndGlobalAgent(t *testing.T) {
	p := instructionImpactFixture()
	p.Kind, p.SourceRef, p.SourceRevisionRef = cp.RevisionImpactKind_REVISION_IMPACT_KIND_PROMPT_TEMPLATE, "mcfg_fixture01", ""
	p.State, p.Version, p.PublishedRevisionRef = cp.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED, 2, p.DraftRef
	plan, ok := revisionImpactPlanView(p)
	if !ok {
		t.Fatal("first prompt publication rejected")
	}
	item := &cp.RevisionImpactItem{Ref: "item_fixture01", ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_fixture01", ConsumerKind: cp.RevisionImpactConsumerKind_REVISION_IMPACT_CONSUMER_KIND_AGENT_CONTINUATION, ConsumerRef: "agt_fixture01", ConsumerVersion: 3, BindingRef: "binding_fixture01", BindingVersion: 2, SourceRevisionRef: "ins_previous01", Outcome: cp.RevisionImpactOutcome_REVISION_IMPACT_OUTCOME_APPLIED, ResultRevisionRef: p.DraftRef, ResultBindingRef: "binding_fixture01", ResultBindingVersion: 3, ResultConsumerVersion: 3}
	if _, ok := revisionImpactItemView(item, plan); !ok {
		t.Fatal("owner binding-only change rejected")
	}
	for _, kind := range []cp.RevisionImpactConsumerKind{cp.RevisionImpactConsumerKind_REVISION_IMPACT_CONSUMER_KIND_WORKFLOW, cp.RevisionImpactConsumerKind(99)} {
		bad := proto.Clone(item).(*cp.RevisionImpactItem)
		bad.ConsumerKind = kind
		if _, ok := revisionImpactItemView(bad, plan); ok {
			t.Fatal("invalid global consumer accepted")
		}
	}
	item.ResultBindingVersion = 2
	if _, ok := revisionImpactItemView(item, plan); ok {
		t.Fatal("unchanged binding accepted")
	}
}

// TestAgentInstructionsBindingPreservesFalseAndRejectsBrokenPin checks that false effective status
// survives projection and a binding without its pinned revision is rejected.
// TestAgentInstructionsBindingPreservesFalseAndRejectsBrokenPin проверяет сохранение
// effective=false при проекции и отказ привязке без закреплённой ревизии.
func TestAgentInstructionsBindingPreservesFalseAndRejectsBrokenPin(t *testing.T) {
	binding := &cp.AgentInstructionsBinding{Ref: "binding_fixture01", Version: 2, RevisionRef: "ins_fixture01", Effective: false}
	value, err := messageMap(binding)
	if err != nil || value["effective"] != false {
		t.Fatalf("false lost: %#v %v", value, err)
	}
	binding.RevisionRef = ""
	if _, err = messageMap(binding); err == nil {
		t.Fatal("missing revision accepted")
	}
}
