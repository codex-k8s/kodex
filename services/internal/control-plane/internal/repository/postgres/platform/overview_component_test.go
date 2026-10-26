package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOverviewEligibilityComponent compares full overview totals with authoritative catalogs,
// exercises native membership revocation and signed project fences, and finds an older active run
// behind more than twenty newer terminal runs. All business states come from ordinary commands.
// TestOverviewEligibilityComponent сравнивает полные счётчики обзора с авторитетными каталогами,
// проверяет штатный отзыв членства и границы проекта, а также старый активный запуск за более чем
// двадцатью новыми завершёнными. Все бизнес-состояния создаются обычными командами.
func TestOverviewEligibilityComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated overview database")
	}
	defer pool.Close()
	r, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "overview-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("overview fixture command %s: not_found=%t forbidden=%t invalid=%t conflict=%t unavailable=%t", kind, errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrForbidden), errors.Is(err, errs.ErrInvalid), errors.Is(err, errs.ErrConflict), errors.Is(err, errs.ErrUnavailable))
		}
		return result
	}
	a := execute(command.CreateProject, "project-a", nil, command.ProjectInput{Name: "Overview visible", Language: "en"}).Project
	b := execute(command.CreateProject, "project-b", nil, command.ProjectInput{Name: "Overview hidden", Language: "en"}).Project
	agentA := createLifecycleAgent(t, ctx, service, owner, a.Ref, "overview-agent-a", "Visible analyst")
	agentB := createLifecycleAgent(t, ctx, service, owner, b.Ref, "overview-agent-b", "Hidden analyst")

	// Штатный legacy workflow создаёт настоящий OPEN Gate до появления других QUEUED запусков.
	agentB = *execute(command.ChangeAgentCapability, "delegate", &agentB.Version, command.AgentBindingInput{AgentRef: agentB.Ref, BindingRef: "platform.run.delegate", Enabled: true}).Agent
	draft := entity.WorkflowVersion{Name: "Overview gated result", CoordinatorAgentRef: agentB.Ref, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "review", Position: 1, Name: "Review", AgentRef: agentB.Ref, Instructions: "Return a verified bounded result.", ExpectedResult: "A verified result", TimeoutSeconds: 600, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "CANCEL"}}}}
	workflow := execute(command.CreateWorkflow, "workflow", nil, command.WorkflowInput{ProjectRef: b.Ref, Name: draft.Name, CoordinatorAgentRef: agentB.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	gated := execute(command.LaunchRun, "gated", nil, command.LaunchRunInput{ProjectRef: b.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Return a bounded result for owner approval."}).Run
	runtimeExecute := func(kind command.Kind, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "overview-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("overview native gate command %s failed", kind)
		}
		return result
	}
	claim := func(key, run string) map[string]any {
		t.Helper()
		items := runtimeExecute(command.ClaimExecution, key, command.LeaseInput{WorkloadInstance: "overview-fixture", Limit: 1}).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run {
			t.Fatal("overview exact native claim missing")
		}
		return items[0]
	}
	complete := func(key string, lease map[string]any) {
		runtimeExecute(command.CompleteExecution, key, command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Verified bounded overview result", Usage: turnUsageFixture()})
	}
	initial := claim("manager-claim", gated.Ref)
	child := runtimeExecute(command.DelegateExecution, "delegate-child", command.DelegateInput{LeaseRef: stringMap(initial, "leaseRef"), Fence: stringMap(initial, "fence"), Generation: runtimeRevisionMapInt64(initial, "generation"), TargetAgentRef: agentB.Ref, WorkflowStepKey: "review", Task: "Review the bounded result."}).Run
	complete("manager-complete", initial)
	complete("child-complete", claim("child-claim", child.Ref))
	gates, total, _, err := service.ListOwnerGates(ctx, owner, query.Filter{ProjectRef: b.Ref, State: "OPEN", Page: query.Page{Size: 20}})
	if err != nil || total != 1 || len(gates) != 1 {
		t.Fatal("overview native owner gate missing")
	}

	// OIDC onboarding и членство проходят штатные команды без SQL-записей готовых прав.
	identity := platformrepo.ProofPrincipalInput{ExternalActorID: uuid.NewString(), ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Overview limited reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.overview.get"}
	if _, err := r.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound overview identity gained authority")
	}
	candidates, _, err := service.ListPlatformMembershipCandidates(ctx, owner, query.Filter{Query: identity.ExternalDisplayName, Page: query.Page{Size: 20}})
	if err != nil || len(candidates) != 1 {
		t.Fatal("overview OIDC candidate missing")
	}
	subjectRef := candidates[0].Ref
	execute(command.AddPlatformMembership, "platform-member", nil, command.PlatformMembershipInput{UserRef: subjectRef, Role: "OPERATOR", Active: true})
	membership := execute(command.AddMembership, "project-member", nil, command.MembershipInput{ProjectRef: a.Ref, UserRef: subjectRef, Permissions: []string{"VIEW"}, Active: true}).Membership
	principal := func(project string) value.Principal {
		t.Helper()
		input := identity
		input.ProjectRef = project
		authority, err := r.ResolveProofAuthority(ctx, input)
		if err != nil {
			t.Fatal("resolve current overview reader authority")
		}
		return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, ProjectRef: authority.ProjectID, Permission: identity.Operation, CallerWorkload: identity.CallerWorkload, CredentialRevision: 1, CorrelationRef: "overview-native"}
	}
	reader := principal("")
	overview := func(actor value.Principal, project string) platformrepo.Overview {
		t.Helper()
		result, err := service.GetOverview(ctx, actor, project)
		if err != nil {
			t.Fatalf("overview read: not_found=%t forbidden=%t unavailable=%t", errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrForbidden), errors.Is(err, errs.ErrUnavailable))
		}
		return result
	}
	assertCatalogParity := func(actor value.Principal, project string) platformrepo.Overview {
		t.Helper()
		result := overview(actor, project)
		agents, next, err := service.ListAgents(ctx, actor, query.Filter{ProjectRef: project, Page: query.Page{Size: 100}})
		if err != nil || next != "" || int(result.AgentCount) != len(agents) {
			t.Fatal("overview agent count differs from canonical SQL plus Go access evaluation")
		}
		_, runTotal, _, err := service.ListRuns(ctx, actor, query.Filter{ProjectRef: project, States: []string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, Page: query.Page{Size: 20}})
		if err != nil || int64(result.ActiveRunCount) != runTotal {
			t.Fatal("overview active count differs from canonical catalog total")
		}
		_, gateTotal, _, err := service.ListOwnerGates(ctx, actor, query.Filter{ProjectRef: project, State: "OPEN", Page: query.Page{Size: 20}})
		if err != nil || int64(result.PendingGateCount) != gateTotal {
			t.Fatal("overview gate count differs from canonical catalog total")
		}
		for _, run := range result.ActiveRuns {
			if !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, run.State) || project != "" && run.ProjectRef != project {
				t.Fatal("overview active array contains an ineligible run")
			}
		}
		return result
	}
	visible := assertCatalogParity(reader, "")
	if visible.ProjectCount != 1 || visible.AgentCount != 1 || visible.PendingGateCount != 0 || len(visible.PendingGates) != 0 {
		t.Fatal("overview leaked hidden project resources")
	}
	all := assertCatalogParity(owner, "")
	if all.ProjectCount != 2 || all.AgentCount != 2 || all.PendingGateCount != 1 {
		t.Fatal("owner overview totals lost native resources")
	}
	for _, ref := range []string{b.Ref, "prj_missing_overview"} {
		if _, err := service.GetOverview(ctx, reader, ref); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("selected hidden overview did not deny")
		}
	}
	scoped := principal(a.Ref)
	if result := assertCatalogParity(scoped, ""); result.ProjectCount != 1 || result.AgentCount != 1 || result.PendingGateCount != 0 {
		t.Fatal("signed project overview escaped authority")
	}
	if _, err := service.GetOverview(ctx, scoped, b.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("selected overview overrode signed project authority")
	}
	foreign := reader
	foreign.AuthorityTenant = uuid.NewString()
	if _, err := service.GetOverview(ctx, foreign, ""); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("overview accepted foreign tenant")
	}

	launch := func(key string) *entity.Run {
		return execute(command.LaunchRun, key, nil, command.LaunchRunInput{ProjectRef: a.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agentA.Ref}, Task: "A bounded queued overview task."}).Run
	}
	old := launch("old-active")
	for index := range 21 {
		key := fmt.Sprintf("terminal-%02d", index)
		run := launch(key)
		execute(command.CancelRun, key+"-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic terminal overview fixture"})
	}
	result := assertCatalogParity(reader, a.Ref)
	if result.ActiveRunCount != 1 || len(result.ActiveRuns) != 1 || result.ActiveRuns[0].Ref != old.Ref {
		t.Fatal("overview lost older active run behind terminal page")
	}
	for index := range 21 {
		launch(fmt.Sprintf("active-%02d", index))
	}
	result = assertCatalogParity(reader, a.Ref)
	if result.ActiveRunCount != 22 || len(result.ActiveRuns) != 20 {
		t.Fatal("overview replaced full active total with bounded list length")
	}
	for index := range 21 {
		key := fmt.Sprintf("extra-agent-%02d", index)
		createLifecycleAgent(t, ctx, service, owner, a.Ref, "overview-"+key, key)
	}
	result = assertCatalogParity(reader, a.Ref)
	if result.AgentCount != 22 {
		t.Fatal("overview agent total was limited to first page")
	}

	// Ресурсное право AGENT без PROJECT view проверяет дополнительный Go predicate каталога.
	exact, exactSubject := previewAuthorityActor(t, ctx, r, service, owner, "overview-exact-reader", []string{"agent.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: a.Ref, ResourceKind: "AGENT", ResourceRef: agentA.Ref})
	if exactSubject == "" {
		t.Fatal("overview exact reader subject missing")
	}
	if result := assertCatalogParity(exact, ""); result.ProjectCount != 0 || result.AgentCount != 1 {
		t.Fatal("overview resource-instance count expanded project scope")
	}
	execute(command.RemoveMembership, "revoke", &membership.Version, command.MembershipInput{ProjectRef: a.Ref, MembershipRef: membership.Ref})
	if result := assertCatalogParity(reader, ""); result.ProjectCount != 0 || result.AgentCount != 0 || result.ActiveRunCount != 0 || result.PendingGateCount != 0 {
		t.Fatal("overview retained revoked visibility")
	}
	if _, err := service.GetOverview(ctx, reader, a.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("selected overview survived membership revoke")
	}
	currentB, err := service.GetProject(ctx, owner, b.Ref)
	if err != nil {
		t.Fatal("read project before native trash")
	}
	execute(command.TrashProject, "trash-b", &currentB.Version, command.ProjectLifecycleInput{Ref: b.Ref})
	if _, err := service.GetOverview(ctx, owner, b.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("selected overview exposed trashed project")
	}
	if result := assertCatalogParity(owner, ""); result.ProjectCount != 1 || result.AgentCount != 22 {
		t.Fatal("organization overview counted trashed project resources")
	}
}
