package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// secretPlanFixture returns a fresh prepared owner-plan snapshot with bounded versions/count and an expiry one hour ahead for local transport tests. It contains no secret value and is not a persisted or approved plan.
// secretPlanFixture возвращает новый prepared owner-plan snapshot с ограниченными versions/count и expiry через час для локальных transport tests. Значения секрета не содержит и сохранённым либо подтверждённым планом не является.
func secretPlanFixture() *cp.RuntimeSecretDraftImpactPlan {
	return &cp.RuntimeSecretDraftImpactPlan{Ref: "sdip_fixture01", DraftRef: "sdft_fixture01", DraftVersion: 3, SecretRef: "sec_fixture01", SecretVersion: 7, SourceRevision: 2, Digest: strings.Repeat("a", 64), Total: 3, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)), State: cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_PREPARED}
}

// secretPlanItemFixture returns a fresh pending project consumer with explicit environment, agent and binding lineage plus two secret revision refs. These are synthetic metadata pins, not actual secret contents or runtime grants.
// secretPlanItemFixture возвращает новый pending project consumer с явными environment, agent и binding lineage и двумя secret revision refs. Это synthetic metadata pins, не содержимое секретов и не runtime grants.
func secretPlanItemFixture() *cp.RuntimeSecretDraftImpactItem {
	return &cp.RuntimeSecretDraftImpactItem{Ref: "sdit_fixture01", Outcome: cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_PENDING, Consumer: &cp.RuntimeSecretImpactConsumer{EnvironmentRef: "renv_fixture01", EnvironmentVersion: 4, EnvironmentVersionRef: "renvv_fixture01", ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", ProjectRef: "prj_fixture01", SecretRevisions: []int64{1, 2}, Consumer: &cp.RuntimeEnvironmentConsumer{ProjectRef: "prj_fixture01", ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", VersionRef: "renvv_fixture01", AgentRef: "agt_fixture01", AgentVersion: 5, BindingRef: "renvb_fixture01", BindingVersion: 6}}}
}

// secretPlanRecorder records a cloned request/method and produces controllable in-memory plan replies or an injected failure. It supports only the tested prepare/query response shapes and does not connect to an owner database or broker.
// secretPlanRecorder фиксирует клонированный request/method и создаёт управляемые in-memory plan replies либо заданный отказ. Поддерживает только проверяемые prepare/query response shapes, к owner database и broker не подключается.
type secretPlanRecorder struct {
	grpc.ClientConnInterface
	request proto.Message
	method  string
	failure error
	mutate  func(*cp.GetRuntimeSecretDraftImpactResponse)
}

// Invoke captures request identity before failure handling, optionally mutates a fresh fixture and fills the selected typed prepare/query reply. It simulates transport responses, not owner authorization, expiry reconciliation or state transitions.
// Invoke сохраняет request identity до обработки отказа, необязательно меняет новый fixture и заполняет выбранный typed prepare/query reply. Имитирует transport responses, не owner authorization, expiry reconciliation и state transitions.
func (c *secretPlanRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	c.request = proto.Clone(request.(proto.Message))
	c.method = method
	if c.failure != nil {
		return c.failure
	}
	result := &cp.GetRuntimeSecretDraftImpactResponse{Plan: secretPlanFixture(), Items: []*cp.RuntimeSecretDraftImpactItem{secretPlanItemFixture()}, Total: 2, Page: &cp.PageInfo{NextPageToken: "fixture-cursor"}}
	if c.mutate != nil {
		c.mutate(result)
	}
	switch out := response.(type) {
	case *cp.PrepareRuntimeSecretDraftImpactResponse:
		out.Plan = result.Plan
	case *cp.GetRuntimeSecretDraftImpactResponse:
		proto.Merge(out, result)
	}
	return nil
}

// secretPlanHandler constructs the generated HTTP router with query/command clients bound to the in-memory recorder, keeping route and request conversion real while excluding live service calls.
// secretPlanHandler создаёт generated HTTP router с query/command clients на in-memory recorder, сохраняя настоящий route и request conversion без live service calls.
func secretPlanHandler(c *secretPlanRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c), Command: cp.NewPlatformCommandServiceClient(c)}})
}

// TestSecretDraftImpactPreservesPlanAndServerFiltering pins prepare draft/version/idempotency, exact query/page forwarding and separate immutable-plan versus filtered totals with no-store output. This is synthetic route/metadata coverage, not execution of the owner's plan transaction or environment replacement.
// TestSecretDraftImpactPreservesPlanAndServerFiltering закрепляет prepare draft/version/idempotency, точную передачу query/page и различие immutable-plan и filtered totals с no-store output. Это synthetic route/metadata coverage, не исполнение owner plan transaction и не замена окружения.
func TestSecretDraftImpactPreservesPlanAndServerFiltering(t *testing.T) {
	c := &secretPlanRecorder{}
	w := httptest.NewRecorder()
	secretPlanHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/impact-plans", ""))
	input, ok := c.request.(*cp.PrepareRuntimeSecretDraftImpactRequest)
	if w.Code != 201 || !ok || input.DraftRef != "sdft_fixture01" || input.Mutation.GetExpectedVersion() != 3 || input.Mutation.IdempotencyKey != "managed-fixture-01" {
		t.Fatalf("plan prepare lost owner fences: %d", w.Code)
	}
	c = &secretPlanRecorder{}
	w = httptest.NewRecorder()
	secretPlanHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01?query=fixture&pageSize=9&pageToken=bound-cursor", ""))
	query, ok := c.request.(*cp.GetRuntimeSecretDraftImpactRequest)
	if w.Code != 200 || !ok || query.PlanRef != "sdip_fixture01" || query.Query != "fixture" || query.Page.PageSize != 9 || query.Page.PageToken != "bound-cursor" {
		t.Fatalf("plan query/cursor not forwarded: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"total":3`) || !strings.Contains(w.Body.String(), `"total":2`) || !strings.Contains(w.Body.String(), `"nextPageToken":"fixture-cursor"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("immutable count mixed with filtered count")
	}
}

// TestSecretDraftImpactFinalOutcomesAndBindingPins covers environment-only and agent-bound applied results plus conflict/forbidden/not-selected outcomes, preserving terminal versus prepared-state rules. Synthetic result pins exercise the public mapper without changing a real environment or binding.
// TestSecretDraftImpactFinalOutcomesAndBindingPins проверяет environment-only и agent-bound applied results, conflict/forbidden/not-selected outcomes и различие terminal и prepared-state rules. Synthetic result pins проверяют public mapper без изменения реального environment и binding.
func TestSecretDraftImpactFinalOutcomesAndBindingPins(t *testing.T) {
	for _, applied := range []bool{false, true} {
		item := secretPlanItemFixture()
		item.Consumer.Consumer = &cp.RuntimeEnvironmentConsumer{ProjectRef: "prj_fixture01", ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", VersionRef: item.Consumer.EnvironmentVersionRef}
		state := generated.RuntimeSecretDraftImpactPlanState("PREPARED")
		if applied {
			state = "APPLIED"
			item.Outcome = cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_APPLIED
			item.ResultEnvironmentVersionRef = "renvv_result01"
		}
		view, ok := secretDraftImpactItemView(item, state)
		if !ok || view.Consumer.ProjectRef != "prj_fixture01" || view.Consumer.Consumer != nil || view.ResultBindingRef != nil {
			t.Fatal("environment-only item lost project lineage or gained binding")
		}
	}
	for _, outcome := range []cp.RuntimeSecretDraftImpactOutcome{cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_APPLIED, cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_CONFLICT, cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_FORBIDDEN, cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_NOT_SELECTED} {
		c := &secretPlanRecorder{mutate: func(r *cp.GetRuntimeSecretDraftImpactResponse) {
			r.Plan.State = cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_APPLIED
			i := r.Items[0]
			i.Outcome = outcome
			if outcome == cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_APPLIED {
				i.ResultEnvironmentVersionRef = "renvv_result01"
				i.ResultBindingRef = i.Consumer.Consumer.BindingRef
				i.ResultBindingVersion = 7
			}
		}}
		w := httptest.NewRecorder()
		secretPlanHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01", ""))
		if w.Code != 200 {
			t.Fatalf("final outcome rejected: %s %d", outcome, w.Code)
		}
	}
	for _, state := range []cp.RuntimeSecretDraftImpactState{cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_PREPARED, cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_CANCELLED, cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_EXPIRED} {
		i := secretPlanItemFixture()
		i.Outcome = cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_NOT_SELECTED
		if _, ok := secretDraftImpactItemView(i, generated.RuntimeSecretDraftImpactPlanState(strings.TrimPrefix(state.String(), "RUNTIME_SECRET_DRAFT_IMPACT_STATE_"))); !ok {
			t.Fatal("unselected item lost before terminal activation")
		}
	}
}

// TestSecretDraftImpactRejectsBrokenOwnerSnapshot rejects malformed plan refs/versions/digest/size/state, unknown or duplicate items, excessive filtered totals and inconsistent effect/binding results with 502. It mutates independent fixture replies, not authoritative plan state.
// TestSecretDraftImpactRejectsBrokenOwnerSnapshot отклоняет неверные plan refs/versions/digest/size/state, unknown и duplicate items, чрезмерные filtered totals и несогласованные effect/binding results с 502. Меняет независимые fixture replies, не авторитетное состояние плана.
func TestSecretDraftImpactRejectsBrokenOwnerSnapshot(t *testing.T) {
	for name, mutate := range map[string]func(*cp.GetRuntimeSecretDraftImpactResponse){
		"missing plan":            func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan = nil },
		"wrong plan":              func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.Ref = "sdip_other01" },
		"missing version":         func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.SecretVersion = 0 },
		"bad digest":              func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.Digest = "broken" },
		"oversized plan":          func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.Total = 1001 },
		"unknown state":           func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.State = 99 },
		"unknown outcome":         func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Items[0].Outcome = 99 },
		"duplicate item":          func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Items = append(r.Items, r.Items[0]) },
		"missing item":            func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Items[0] = nil },
		"filtered count overflow": func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Total = 4 },
		"pending after applied": func(r *cp.GetRuntimeSecretDraftImpactResponse) {
			r.Plan.State = cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_APPLIED
		},
		"effect before activation": func(r *cp.GetRuntimeSecretDraftImpactResponse) {
			r.Items[0].Outcome = cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_APPLIED
		},
		"unexpected effect fields": func(r *cp.GetRuntimeSecretDraftImpactResponse) {
			r.Items[0].ResultEnvironmentVersionRef = "renvv_result01"
		},
		"stale binding": func(r *cp.GetRuntimeSecretDraftImpactResponse) {
			r.Plan.State = cp.RuntimeSecretDraftImpactState_RUNTIME_SECRET_DRAFT_IMPACT_STATE_APPLIED
			i := r.Items[0]
			i.Outcome = cp.RuntimeSecretDraftImpactOutcome_RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_APPLIED
			i.ResultEnvironmentVersionRef = "renvv_result01"
			i.ResultBindingRef = i.Consumer.Consumer.BindingRef
			i.ResultBindingVersion = i.Consumer.Consumer.BindingVersion
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := &secretPlanRecorder{mutate: mutate}
			w := httptest.NewRecorder()
			secretPlanHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01", ""))
			if w.Code != 502 {
				t.Fatalf("invalid plan accepted: %d", w.Code)
			}
		})
	}
}

// TestSecretDraftImpactRejectsStaleCursorAndAuthorityWithoutFilteringLocally preserves mapped owner denials for stale-cursor requests, rejects unbounded query/page before RPC and detects a changed prepare draft-version pin. The recorder injects owner errors; the test does not itself implement cursor signing or actor authorization.
// TestSecretDraftImpactRejectsStaleCursorAndAuthorityWithoutFilteringLocally сохраняет mapping owner denials для stale-cursor requests, отклоняет unbounded query/page до RPC и обнаруживает изменённый prepare draft-version pin. Recorder задаёт owner errors; сам тест cursor signing и actor authorization не реализует.
func TestSecretDraftImpactRejectsStaleCursorAndAuthorityWithoutFilteringLocally(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.InvalidArgument: 400, codes.NotFound: 404, codes.PermissionDenied: 403, codes.Aborted: 412, codes.Unavailable: 503} {
		c := &secretPlanRecorder{failure: status.Error(code, "private owner detail")}
		w := httptest.NewRecorder()
		secretPlanHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01?pageToken=stale", ""))
		if w.Code != expected || strings.Contains(w.Body.String(), "private owner") {
			t.Fatal("owner denial became success")
		}
	}
	for _, suffix := range []string{"?pageSize=101", "?pageSize=0", "?query=" + strings.Repeat("x", 201)} {
		c := &secretPlanRecorder{}
		w := httptest.NewRecorder()
		secretPlanHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01"+suffix, ""))
		if w.Code != 400 || c.request != nil {
			t.Fatal("unbounded request reached owner")
		}
	}
	c := &secretPlanRecorder{mutate: func(r *cp.GetRuntimeSecretDraftImpactResponse) { r.Plan.DraftVersion = 4 }}
	w := httptest.NewRecorder()
	secretPlanHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/impact-plans", ""))
	if w.Code != 502 {
		t.Fatal("prepare response lost requested draft pin")
	}
}

// TestSecretDraftPublishRejectsCallerInventedOrDuplicatePlanItems blocks absent plan refs, nil selections and malformed/duplicate selected refs before owner RPC, while preserving explicit empty selection for no-replacement publication. It checks transport selection semantics, not live publication or plan item membership in storage.
// TestSecretDraftPublishRejectsCallerInventedOrDuplicatePlanItems блокирует отсутствие plan ref, nil selections и неверные/повторные selected refs до owner RPC, сохраняет явный пустой набор для no-replacement publication. Проверяет transport selection semantics, не live publication и принадлежность items плану в хранилище.
func TestSecretDraftPublishRejectsCallerInventedOrDuplicatePlanItems(t *testing.T) {
	for _, body := range []string{
		`{"expectedSecretVersion":7,"selectedItemRefs":[]}`,
		`{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01"}`,
		`{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":null}`,
		`{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":["sdit_fixture01","sdit_fixture01"]}`,
		`{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":["bad ref"]}`,
	} {
		c := &secretDraftRecorder{}
		w := httptest.NewRecorder()
		secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/publish", body))
		if w.Code != 400 || len(c.calls) != 0 {
			t.Fatal("invalid plan selection reached owner")
		}
	}
	c := &secretDraftRecorder{}
	w := httptest.NewRecorder()
	secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/publish", `{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":[]}`))
	if w.Code != 200 {
		t.Fatal("explicit no-replacement publication rejected")
	}
	input := c.requests[0].(*cp.PreparePublishRuntimeSecretDraftRequest)
	if input.ImpactPlanRef != "sdip_fixture01" || len(input.SelectedItemRefs) != 0 {
		t.Fatal("empty selected set changed")
	}
}
