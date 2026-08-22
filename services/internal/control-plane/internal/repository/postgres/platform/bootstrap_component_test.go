package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed sql/bootstrap_component_readback.sql
	bootstrapComponentReadbackQuery string
	//go:embed sql/bootstrap_component_disable_system_assistant.sql
	bootstrapComponentDisableSystemAssistantQuery string
	//go:embed sql/bootstrap_component_delete_system_assistant.sql
	bootstrapComponentDeleteSystemAssistantQuery string
	//go:embed sql/bootstrap_component_replace_core_prompt.sql
	bootstrapComponentReplaceCorePromptQuery string
	//go:embed sql/bootstrap_component_replace_session_provider_account.sql
	bootstrapComponentReplaceSessionProviderAccountQuery string
)

func TestBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("MATTERCODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("MATTERCODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5")
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatalf("configure provider credential: %v", err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/mattercodex/staging", PromotedRepository: "registry.invalid/mattercodex/roles",
		DefaultImageReference: "registry.invalid/mattercodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatalf("configure role images: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	assertBootstrapReadback(t, ctx, pool)

	for name, query := range map[string]string{
		"disable system assistant":         bootstrapComponentDisableSystemAssistantQuery,
		"delete system assistant":          bootstrapComponentDeleteSystemAssistantQuery,
		"replace core prompt":              bootstrapComponentReplaceCorePromptQuery,
		"replace session provider account": bootstrapComponentReplaceSessionProviderAccountQuery,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("protected system state was changed")
			}
		})
	}
	assertBootstrapReadback(t, ctx, pool)
	t.Run("OIDC candidate receives project membership without internal identifiers", func(t *testing.T) {
		testProjectMembershipCandidate(t, ctx, repository)
	})
	t.Run("system assistant proposes and applies typed plan", func(t *testing.T) {
		testSystemAssistantTypedPlan(t, ctx, repository)
	})
	t.Run("direct run continuation cancel and retry", func(t *testing.T) {
		testDirectRunLifecycle(t, ctx, repository)
	})
	t.Run("provider neutral nested delegation", func(t *testing.T) {
		testNestedDelegation(t, ctx, repository)
	})
	t.Run("human gate resolves once and completes root", func(t *testing.T) {
		testHumanGateLifecycle(t, ctx, repository)
	})
	t.Run("idempotency occ and concurrent run creation", func(t *testing.T) {
		testIdempotencyOCCAndConcurrentRuns(t, ctx, repository)
	})
}

func testProjectMembershipCandidate(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Installation owner", ExternalEmailHint: "o***@example.test",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct membership service: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-project-create"},
		Payload: command.ProjectInput{Name: "Access validation", Purpose: "Validate member onboarding", Language: "en"},
	})
	if err != nil || created.Project == nil {
		t.Fatalf("create membership project: project=%#v err=%v", created.Project, err)
	}
	projectRef := created.Project.Ref
	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000003", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Alex Morgan", ExternalEmailHint: "a***@example.test",
		CallerWorkload: "control-api-gateway", Operation: "platform.query.membership-candidates.list", ProjectRef: projectRef,
	}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unknown OIDC subject received authority before membership: %v", err)
	}
	candidates, _, err := service.ListMembershipCandidates(ctx, owner, query.Filter{ProjectRef: projectRef, Query: "Alex", Page: query.Page{Size: 20}})
	if err != nil || len(candidates) != 1 || candidates[0].DisplayName != candidateInput.ExternalDisplayName || candidates[0].EmailMasked != candidateInput.ExternalEmailHint {
		t.Fatalf("list membership candidate: candidates=%#v err=%v", candidates, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.AddMembership, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-system-subject-rejected"},
		Payload: command.MembershipInput{ProjectRef: projectRef, UserRef: "sys_platform", Role: "OPERATOR", Permissions: []string{"VIEW"}, Active: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("system subject accepted as project member: %v", err)
	}
	added, err := service.Execute(ctx, command.Command{
		Kind: command.AddMembership, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-candidate-add"},
		Payload: command.MembershipInput{ProjectRef: projectRef, UserRef: candidates[0].Ref, Role: "OPERATOR", Permissions: []string{"VIEW", "MANAGE_MEMBERS"}, Active: true},
	})
	if err != nil || added.Membership == nil || !added.Membership.Active {
		t.Fatalf("add project membership: membership=%#v err=%v", added.Membership, err)
	}
	candidateAuthority, err := repository.ResolveProofAuthority(ctx, candidateInput)
	if err != nil {
		t.Fatalf("resolve candidate after membership: %v", err)
	}
	candidate := value.Principal{
		ActorID: candidateAuthority.ActorID, AuthorityTenant: candidateAuthority.OrganizationID,
		Permission: candidateInput.Operation, CorrelationRef: "membership-candidate-component",
		CallerWorkload: "control-api-gateway", ProjectRef: projectRef, CredentialRevision: 1,
	}
	memberships, _, err := service.ListMemberships(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(memberships) != 2 {
		t.Fatalf("member cannot use granted project permission: memberships=%d err=%v", len(memberships), err)
	}
	remaining, _, err := service.ListMembershipCandidates(ctx, owner, query.Filter{ProjectRef: projectRef, Query: "Alex", Page: query.Page{Size: 20}})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("assigned member remained a candidate: candidates=%#v err=%v", remaining, err)
	}
	foreign, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-foreign-project"},
		Payload: command.ProjectInput{Name: "Foreign access validation", Purpose: "Validate project isolation", Language: "en"},
	})
	if err != nil || foreign.Project == nil {
		t.Fatalf("create foreign project: project=%#v err=%v", foreign.Project, err)
	}
	foreignInput := candidateInput
	foreignInput.ProjectRef = foreign.Project.Ref
	if _, err := repository.ResolveProofAuthority(ctx, foreignInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("candidate received foreign project authority: %v", err)
	}
}

func testIdempotencyOCCAndConcurrentRuns(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct idempotency service: %v", err)
	}
	projectCommand := command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-1"}, Payload: command.ProjectInput{
			Name: "Procurement", Purpose: "Coordinate supplier selection", Language: "en",
		}}
	first, err := service.Execute(ctx, projectCommand)
	if err != nil || first.Project == nil {
		t.Fatalf("create idempotent project: result=%#v err=%v", first.Project, err)
	}
	replayed, err := service.Execute(ctx, projectCommand)
	if err != nil || replayed.Project == nil || replayed.Project.Ref != first.Project.Ref {
		t.Fatalf("replay identical project intent: result=%#v err=%v", replayed.Project, err)
	}
	different := projectCommand
	different.Payload = command.ProjectInput{Name: "Different project", Purpose: "Different intent", Language: "en"}
	if _, err := service.Execute(ctx, different); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("reuse idempotency key with different intent: %v", err)
	}
	projectVersion := first.Project.Version
	updated, err := service.Execute(ctx, command.Command{Kind: command.UpdateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-update", ExpectedVersion: &projectVersion},
		Payload:  command.ProjectInput{Ref: first.Project.Ref, Name: "Supplier procurement", Purpose: "Select and onboard suppliers", Language: "en"},
	})
	if err != nil || updated.Project == nil || updated.Project.Version != projectVersion+1 {
		t.Fatalf("update project with current version: result=%#v err=%v", updated.Project, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.UpdateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-stale-update", ExpectedVersion: &projectVersion},
		Payload:  command.ProjectInput{Ref: first.Project.Ref, Name: "Stale update", Purpose: "Must not apply", Language: "en"},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("accept stale project version: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, first.Project.Ref, "concurrent-run-agent", "Procurement analyst")
	type runResult struct {
		result command.Result
		err    error
	}
	sharedCommand := command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "concurrent-same-intent-run"}, Payload: command.LaunchRunInput{
			ProjectRef: first.Project.Ref, Title: "Evaluate shared supplier", Task: "Evaluate the same bounded supplier profile.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Input: map[string]any{"supplier": "shared"},
		}}
	sharedResults := make(chan runResult, 2)
	for range 2 {
		go func() {
			result, executeErr := service.Execute(ctx, sharedCommand)
			sharedResults <- runResult{result: result, err: executeErr}
		}()
	}
	sharedRuns := make([]entity.Run, 0, 2)
	for range 2 {
		outcome := <-sharedResults
		if outcome.err != nil || outcome.result.Run == nil {
			t.Fatalf("create same-intent concurrent run: result=%#v err=%v", outcome.result.Run, outcome.err)
		}
		sharedRuns = append(sharedRuns, *outcome.result.Run)
	}
	if sharedRuns[0].Ref != sharedRuns[1].Ref {
		t.Fatalf("same idempotency scope created different runs: %s %s", sharedRuns[0].Ref, sharedRuns[1].Ref)
	}
	sharedVersion := sharedRuns[0].Version
	if cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "concurrent-same-intent-cancel", ExpectedVersion: &sharedVersion},
		Payload:  command.RunCommandInput{RunRef: sharedRuns[0].Ref, Reason: "Component test cleanup"},
	}); err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel same-intent concurrent run: run=%#v err=%v", cancelled.Run, err)
	}
	results := make(chan runResult, 2)
	for index := 1; index <= 2; index++ {
		index := index
		go func() {
			result, executeErr := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: "concurrent-run-" + leftPad(index, 2)}, Payload: command.LaunchRunInput{
					ProjectRef: first.Project.Ref, Title: "Evaluate supplier " + leftPad(index, 2), Task: "Evaluate the bounded supplier profile.",
					Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Input: map[string]any{"supplier": index},
				}})
			results <- runResult{result: result, err: executeErr}
		}()
	}
	createdRuns := make([]entity.Run, 0, 2)
	for range 2 {
		outcome := <-results
		if outcome.err != nil || outcome.result.Run == nil {
			t.Fatalf("create concurrent run: result=%#v err=%v", outcome.result.Run, outcome.err)
		}
		createdRuns = append(createdRuns, *outcome.result.Run)
	}
	if createdRuns[0].Ref == createdRuns[1].Ref {
		t.Fatalf("concurrent run creation returned duplicate ref %s", createdRuns[0].Ref)
	}
	for index := range createdRuns {
		version := createdRuns[index].Version
		cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "concurrent-run-cancel-" + leftPad(index+1, 2), ExpectedVersion: &version},
			Payload:  command.RunCommandInput{RunRef: createdRuns[index].Ref, Reason: "Component test cleanup"},
		})
		if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
			t.Fatalf("cancel concurrent run %d: run=%#v err=%v", index+1, cancelled.Run, err)
		}
	}
}

func testHumanGateLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.owner_gates.resolve",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "mattercodex-system-subject", ExternalTenantID: "mattercodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct gate service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-project-1"}, Payload: command.ProjectInput{
			Name: "Legal review", Purpose: "Review business documents", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create gate project: result=%#v err=%v", project.Project, err)
	}
	reviewer := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "gate-reviewer", "Legal reviewer")
	draft := entity.WorkflowVersion{Ref: "draft", Name: "Contract review", Purpose: "Prepare a contract recommendation",
		CoordinatorAgentRef: reviewer.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600,
		CompletionCriteria: "A recommendation is approved by the owner", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "review", Position: 1, Name: "Review contract", AgentRef: reviewer.Ref,
			Instructions: "Review the contract and prepare a recommendation.", TimeoutSeconds: 900,
			ExpectedResult: "A bounded recommendation", HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}}},
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-create"}, Payload: command.WorkflowInput{
			ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: reviewer.Ref, Draft: &draft,
		}})
	if err != nil || created.Workflow == nil {
		t.Fatalf("create gate workflow: result=%#v err=%v", created.Workflow, err)
	}
	workflowVersion := created.Workflow.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-validate", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: created.Workflow.Ref},
	})
	if err != nil || validated.Workflow == nil || validated.Workflow.State != "VALID" {
		t.Fatalf("validate gate workflow: result=%#v err=%v", validated.Workflow, err)
	}
	workflowVersion = validated.Workflow.Version
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-publish", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: created.Workflow.Ref},
	})
	if err != nil || published.Workflow == nil || published.Workflow.State != "PUBLISHED" {
		t.Fatalf("publish gate workflow: result=%#v err=%v", published.Workflow, err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-run-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Review supplier contract", Task: "Review the attached supplier terms.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: published.Workflow.Ref}, Input: map[string]any{"contract": "supplier-terms"},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch gate workflow: run=%#v err=%v", launched.Run, err)
	}
	waiting := claimAndCompleteRun(t, ctx, service, worker, "gate-review", false)
	if waiting.Run == nil || waiting.Run.State != "WAITING_HUMAN" || len(waiting.Run.GateRefs) != 1 {
		t.Fatalf("open owner gate: run=%#v event=%#v", waiting.Run, waiting.Event)
	}
	gateRef := waiting.Run.GateRefs[0]
	gateVersion := int64(1)
	resolved, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE", Comment: "Approved for use"},
	})
	if err != nil || resolved.Gate == nil || resolved.Gate.State != "APPROVED" || resolved.Run == nil || resolved.Run.State != "SUCCEEDED" {
		t.Fatalf("resolve terminal owner gate: gate=%#v run=%#v err=%v", resolved.Gate, resolved.Run, err)
	}
	if resolved.Graph == nil || graphNodeState(resolved.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("terminal gate did not close the root graph: %#v", resolved.Graph)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-replay", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE", Comment: "Replay"},
	})
	if err == nil {
		t.Fatal("replayed owner gate resolution was accepted")
	}
	changeRun, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-change-run-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Review revised supplier contract", Task: "Review the revised supplier terms.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: published.Workflow.Ref}, Input: map[string]any{"contract": "supplier-terms-revised"},
		}})
	if err != nil || changeRun.Run == nil {
		t.Fatalf("launch change-request workflow: run=%#v err=%v", changeRun.Run, err)
	}
	changeWaiting := claimAndCompleteRun(t, ctx, service, worker, "gate-change-review", false)
	if changeWaiting.Run == nil || changeWaiting.Run.State != "WAITING_HUMAN" || len(changeWaiting.Run.GateRefs) != 1 {
		t.Fatalf("open change-request gate: run=%#v", changeWaiting.Run)
	}
	changeGateVersion := int64(1)
	changes, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-changes", ExpectedVersion: &changeGateVersion},
		Payload: command.GateResolutionInput{GateRef: changeWaiting.Run.GateRefs[0], Decision: "REQUEST_CHANGES",
			Comment: "Add the termination risk and propose a mitigation."},
	})
	if err != nil || changes.Gate == nil || changes.Gate.State != "CHANGES_REQUESTED" || changes.Run == nil || changes.Run.State != "RUNNING" {
		t.Fatalf("request workflow changes: gate=%#v run=%#v err=%v", changes.Gate, changes.Run, err)
	}
	if changes.Graph == nil || graphNodeState(changes.Graph.Nodes, "AGENT_EXECUTION") != "QUEUED" {
		t.Fatalf("requested changes did not requeue the agent node: %#v", changes.Graph)
	}
	reworked := claimAndCompleteRun(t, ctx, service, worker, "gate-change-rework", false)
	if reworked.Run == nil || reworked.Run.State != "WAITING_HUMAN" || len(reworked.Run.GateRefs) != 2 {
		t.Fatalf("open gate after requested changes: run=%#v", reworked.Run)
	}
	secondGateVersion := int64(1)
	finalGateRef := reworked.Run.GateRefs[len(reworked.Run.GateRefs)-1]
	final, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-rework", ExpectedVersion: &secondGateVersion},
		Payload:  command.GateResolutionInput{GateRef: finalGateRef, Decision: "APPROVE", Comment: "Rework approved"},
	})
	if err != nil || final.Run == nil || final.Run.State != "SUCCEEDED" {
		t.Fatalf("approve reworked workflow: run=%#v err=%v", final.Run, err)
	}
}

func testNestedDelegation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "mattercodex-system-subject", ExternalTenantID: "mattercodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct delegation service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-project-1"}, Payload: command.ProjectInput{
			Name: "Content operations", Purpose: "Prepare and review business content", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create delegation project: result=%#v err=%v", project.Project, err)
	}
	runtimes, err := service.ListRuntimes(ctx, owner)
	if err != nil || len(runtimes) != 1 || !runtimes[0].Ready || runtimes[0].Ref != defaultRuntimeKey {
		t.Fatalf("list enabled runtime catalog: runtimes=%#v err=%v", runtimes, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-unknown-runtime"}, Payload: command.AgentInput{
			ProjectRef: project.Project.Ref, Name: "Invalid runtime agent", Purpose: "Must not be created",
			RoleDescription: "Invalid runtime", Instructions: "This instruction is long enough for validation.", RuntimeRef: "runtime_unknown",
		}}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("create agent accepted unknown runtime: %v", err)
	}
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-coordinator", "Content coordinator")
	firstChild := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-researcher", "Research specialist")
	secondChild := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-editor", "Content editor")
	coordinatorVersion := coordinator.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-unknown-capability", ExpectedVersion: &coordinatorVersion},
		Payload:  command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.unknown", Enabled: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("grant accepted unknown capability: %v", err)
	}
	capability, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-capability-1", ExpectedVersion: &coordinatorVersion},
		Payload:  command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true},
	})
	if err != nil || capability.Agent == nil || capability.Agent.Name == "" || !contains(capability.Agent.Capabilities, "platform.run.delegate") {
		t.Fatalf("grant delegation capability: result=%#v err=%v", capability.Agent, err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-launch-1"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Prepare campaign brief", Task: "Coordinate research and editing.",
			Target: entity.RunTarget{Type: "AGENT", Ref: coordinator.Ref}, Input: map[string]any{"campaign": "Autumn"},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch delegation coordinator: run=%#v err=%v", launched.Run, err)
	}
	coordinatorClaim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-coordinator-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(coordinatorClaim.RuntimeItems) != 1 {
		t.Fatalf("claim delegation coordinator: claims=%d err=%v", len(coordinatorClaim.RuntimeItems), err)
	}
	coordinatorLease := coordinatorClaim.RuntimeItems[0]
	delegations := []struct {
		key   string
		agent entity.Agent
	}{
		{key: "delegation-first", agent: firstChild},
		{key: "delegation-second", agent: secondChild},
	}
	children := make([]command.Result, 0, len(delegations))
	for _, item := range delegations {
		delegated, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: item.key}, Payload: command.DelegateInput{
				LeaseRef: stringMap(coordinatorLease, "leaseRef"), Fence: stringMap(coordinatorLease, "fence"),
				Generation: coordinatorLease["generation"].(int64), TargetAgentRef: item.agent.Ref,
				Task: "Complete the assigned part of the campaign brief.", Input: map[string]any{"part": item.key},
			}})
		if err != nil || delegated.Run == nil || stringMap(delegated.Runtime, "callbackEdgeRef") == "" {
			t.Fatalf("delegate %s child: run=%#v runtime=%v err=%v", item.key, delegated.Run, delegated.Runtime, err)
		}
		children = append(children, delegated)
	}
	claimedChildren, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-children-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 2}})
	if err != nil || len(claimedChildren.RuntimeItems) != 2 {
		t.Fatalf("claim delegated children: claims=%d err=%v", len(claimedChildren.RuntimeItems), err)
	}
	for index, lease := range claimedChildren.RuntimeItems {
		completeClaimedExecution(t, ctx, service, worker, lease, "delegation-child-"+leftPad(index+1, 2), false)
	}
	for index, child := range children {
		callback, err := service.Execute(ctx, command.Command{Kind: command.DeliverCallback, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "delegation-callback-replay-" + leftPad(index+1, 2)},
			Payload:  command.CallbackInput{ChildRunRef: child.Run.Ref, CallbackEdgeRef: stringMap(child.Runtime, "callbackEdgeRef")},
		})
		if err != nil || !callback.Duplicate {
			t.Fatalf("deduplicate child callback %d: duplicate=%v err=%v", index+1, callback.Duplicate, err)
		}
	}
	completed := completeClaimedExecution(t, ctx, service, worker, coordinatorLease, "delegation-coordinator", false)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" || completed.Graph == nil || len(completed.Graph.Nodes) < 4 || graphNodeState(completed.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("complete delegation root: run=%#v graph=%#v", completed.Run, completed.Graph)
	}
	callbackEdges := 0
	for _, edge := range completed.Graph.Edges {
		if edge.Type == "CALLBACK_TO" {
			callbackEdges++
		}
	}
	if callbackEdges != 2 {
		t.Fatalf("delegation graph lost callback edges: edges=%#v", completed.Graph.Edges)
	}
}

func graphNodeState(nodes []entity.RunNode, nodeType string) string {
	for _, node := range nodes {
		if node.Type == nodeType {
			return node.State
		}
	}
	return ""
}

func createLifecycleAgent(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, key, name string) entity.Agent {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AgentInput{
			ProjectRef: projectRef, Name: name, Purpose: "Complete a bounded business task", RoleDescription: name,
			Instructions: "Complete only the assigned task and return a concise, verifiable result.",
		}})
	if err != nil || result.Agent == nil {
		t.Fatalf("create %s: result=%#v err=%v", key, result.Agent, err)
	}
	return *result.Agent
}

func testDirectRunLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "mattercodex-system-subject", ExternalTenantID: "mattercodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct lifecycle service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-project-1"}, Payload: command.ProjectInput{
			Name: "Customer support", Purpose: "Resolve customer requests", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create lifecycle project: result=%#v err=%v", project.Project, err)
	}
	agent, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-agent-1"}, Payload: command.AgentInput{
			ProjectRef: project.Project.Ref, Name: "Support specialist", Purpose: "Prepare customer responses",
			RoleDescription: "Customer support specialist", Instructions: "Analyze the request and prepare a clear, safe customer response.",
		}})
	if err != nil || agent.Agent == nil {
		t.Fatalf("create lifecycle agent: result=%#v err=%v", agent.Agent, err)
	}
	uploaded, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-upload-1"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "support-policy.md", MediaType: "application/octet-stream",
		SizeBytes: int64(len("# Support policy\n")), Reader: strings.NewReader("# Support policy\n"),
	})
	if err != nil || uploaded.ScanState != "CLEAN" || uploaded.MediaType != "text/markdown" || uploaded.Revision != 1 || uploaded.Source != "CONTROL_CENTER" {
		t.Fatalf("upload knowledge artifact: artifact=%#v err=%v", uploaded, err)
	}
	preview, err := service.DownloadArtifact(ctx, owner, uploaded.Ref, "PREVIEW")
	if err != nil || preview.GrantRef == "" {
		t.Fatalf("open safe artifact preview: grant=%q err=%v", preview.GrantRef, err)
	}
	previewBody, previewReadErr := io.ReadAll(preview.Reader)
	previewCloseErr := preview.Reader.Close()
	if previewReadErr != nil || previewCloseErr != nil || string(previewBody) != "# Support policy\n" {
		t.Fatalf("read safe artifact preview: body=%q read_err=%v close_err=%v", string(previewBody), previewReadErr, previewCloseErr)
	}
	quarantined, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-quarantine-1"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "unsafe.exe", MediaType: "application/octet-stream",
		SizeBytes: 2, Reader: strings.NewReader("MZ"),
	})
	if err != nil || quarantined.ScanState != "QUARANTINED" {
		t.Fatalf("quarantine executable artifact: artifact=%#v err=%v", quarantined, err)
	}
	if _, err := service.DownloadArtifact(ctx, owner, quarantined.Ref, "DOWNLOAD"); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("download quarantined artifact must be forbidden: %v", err)
	}
	uploadedVersion := uploaded.Version
	bound, err := service.Execute(ctx, command.Command{Kind: command.ChangeArtifactBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "artifact-binding-1", ExpectedVersion: &uploadedVersion},
		Payload:  command.ArtifactBindingInput{ArtifactRef: uploaded.Ref, AgentRef: agent.Agent.Ref, Enabled: true},
	})
	if err != nil || bound.Artifact == nil || bound.Artifact.FileName != uploaded.FileName || len(bound.Artifact.Bindings) != 1 || bound.Artifact.Bindings[0] != agent.Agent.Ref {
		t.Fatalf("bind knowledge artifact: artifact=%#v err=%v", bound.Artifact, err)
	}
	boundAgent, err := service.GetAgent(ctx, owner, agent.Agent.Ref)
	if err != nil || len(boundAgent.KnowledgeArtifactRefs) != 1 || boundAgent.KnowledgeArtifactRefs[0] != uploaded.Ref || boundAgent.Version != agent.Agent.Version+1 {
		t.Fatalf("read normalized knowledge binding: agent=%#v err=%v", boundAgent, err)
	}
	secondRevision, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-upload-2"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "support-policy.md", MediaType: "text/markdown",
		SizeBytes: int64(len("# Updated policy\n")), Reader: strings.NewReader("# Updated policy\n"),
	})
	if err != nil || secondRevision.Revision != 2 {
		t.Fatalf("create second artifact revision: artifact=%#v err=%v", secondRevision, err)
	}
	if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-content-conflict"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "same.txt", MediaType: "text/plain", SizeBytes: 5, Reader: strings.NewReader("alpha"),
	}); err != nil {
		t.Fatalf("create artifact idempotency baseline: %v", err)
	}
	if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-content-conflict"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "same.txt", MediaType: "text/plain", SizeBytes: 5, Reader: strings.NewReader("bravo"),
	}); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("same artifact key with different content: %v", err)
	}
	launch, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-launch-1"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Answer customer", Task: "Prepare an answer about delivery status.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Agent.Ref}, Input: map[string]any{"ticket": "SUP-42"},
		}})
	if err != nil || launch.Run == nil || launch.Graph == nil || launch.Run.State != "RUNNING" || len(launch.Graph.Nodes) != 2 {
		t.Fatalf("launch direct run: run=%#v graph=%#v err=%v", launch.Run, launch.Graph, err)
	}
	completed := claimAndCompleteRun(t, ctx, service, worker, "lifecycle-first", true)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" || len(completed.CreatedRefs) != 1 {
		t.Fatalf("complete direct run: run=%#v artifacts=%v", completed.Run, completed.CreatedRefs)
	}
	if completed.Graph == nil || graphNodeState(completed.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("completed run left the root graph active: %#v", completed.Graph)
	}
	download, err := service.DownloadArtifact(ctx, owner, completed.CreatedRefs[0], "DOWNLOAD")
	if err != nil {
		t.Fatalf("open generated artifact download: %v", err)
	}
	if download.GrantRef == "" {
		t.Fatal("download must materialize a one-time grant")
	}
	body, readErr := io.ReadAll(download.Reader)
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "Customer response is ready.\n" {
		t.Fatalf("download generated artifact: body=%q read_err=%v close_err=%v", string(body), readErr, closeErr)
	}
	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-continuation-1"}, Payload: command.SessionTurnInput{
			SessionRef: launch.Run.SessionRef, RunRef: launch.Run.Ref, Task: "Add a concise follow-up for the customer.",
		}})
	if err != nil || continued.Run == nil || continued.Graph == nil || continued.Run.State != "RUNNING" {
		t.Fatalf("continue session: run=%#v graph=%#v err=%v", continued.Run, continued.Graph, err)
	}
	continuedVersion := continued.Run.Version
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-cancel-1", ExpectedVersion: &continuedVersion},
		Payload:  command.RunCommandInput{RunRef: continued.Run.Ref, Reason: "No longer needed"},
	})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel continued run: run=%#v err=%v", cancelled.Run, err)
	}
	cancelledVersion := cancelled.Run.Version
	retried, err := service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-retry-1", ExpectedVersion: &cancelledVersion},
		Payload:  command.RunCommandInput{RunRef: cancelled.Run.Ref, Reason: "Retry with the same bounded input"},
	})
	if err != nil || retried.Run == nil || retried.Graph == nil || retried.Run.Attempt != 2 || retried.Run.RetryOfRunRef != cancelled.Run.Ref {
		t.Fatalf("retry cancelled run: run=%#v graph=%#v err=%v", retried.Run, retried.Graph, err)
	}
	completedRetry := claimAndCompleteRun(t, ctx, service, worker, "lifecycle-retry", false)
	events, currentSequence, complete, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completedRetry.Run.Ref, Limit: 100})
	if err != nil || !complete || len(events) == 0 || currentSequence != events[len(events)-1].Sequence {
		t.Fatalf("read retry event stream: events=%d sequence=%d complete=%v err=%v", len(events), currentSequence, complete, err)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("non-monotonic event sequence at %d: %d", index, event.Sequence)
		}
		if event.Delta.Run == nil || event.Delta.Run.Ref != event.RunRef || event.Delta.Run.EventSequence != event.Sequence || event.Delta.Run.GraphRevision != event.GraphRevision {
			t.Fatalf("event %d does not carry an authoritative run delta: %#v", event.Sequence, event.Delta.Run)
		}
		if event.NodeRef != "" && (event.Delta.Node == nil || event.Delta.Node.Ref != event.NodeRef) {
			t.Fatalf("event %d lost its node delta: node_ref=%s delta=%#v", event.Sequence, event.NodeRef, event.Delta.Node)
		}
		if event.EdgeRef != "" && (event.Delta.Edge == nil || event.Delta.Edge.Ref != event.EdgeRef) {
			t.Fatalf("event %d lost its edge delta: edge_ref=%s delta=%#v", event.Sequence, event.EdgeRef, event.Delta.Edge)
		}
	}
}

func claimAndCompleteRun(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, key string, artifact bool) command.Result {
	t.Helper()
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim %s execution: claims=%d err=%v", key, len(claimed.RuntimeItems), err)
	}
	return completeClaimedExecution(t, ctx, service, worker, claimed.RuntimeItems[0], key, artifact)
}

func completeClaimedExecution(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, lease map[string]any, key string, artifact bool) command.Result {
	t.Helper()
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportExecutionProgress, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-progress"}, Payload: command.LeaseInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Progress: "Preparing the result",
		}}); err != nil {
		t.Fatalf("report %s progress: %v", key, err)
	}
	artifacts := []command.CompletedArtifact{}
	if artifact {
		content := []byte("Customer response is ready.\n")
		digest := sha256.Sum256(content)
		artifacts = append(artifacts, command.CompletedArtifact{FileName: "customer-response.txt", MediaType: "text/plain",
			SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(content)), Content: content})
	}
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Success: true, ResultSummary: "Customer response prepared", Artifacts: artifacts,
		}})
	if err != nil {
		t.Fatalf("complete %s execution: %v", key, err)
	}
	return completed
}

func testSystemAssistantTypedPlan(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "mattercodex-system-subject", ExternalTenantID: "mattercodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.plan.propose",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportWarmRuntime, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-ready-1"}, Payload: command.WarmRuntimeInput{
			WorkloadInstance: "runtime-test", RuntimeRevision: systemassistant.CorePromptRevision, State: "READY",
		}}); err != nil {
		t.Fatalf("report assistant readiness: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-conversation-1"}, Payload: command.AssistantConversationInput{Title: "Configure sales team"}})
	if err != nil {
		t.Fatalf("create assistant conversation: %v", err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve owner readback: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve owner scope readback: %v", err)
	}
	var conversationID, sessionID, sessionRef, projectID, projectRef string
	var conversationVersion int64
	if err := repository.pool.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState,
		ownerScope.organizationID, created.Conversation.Ref,
	).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &conversationVersion); err != nil {
		t.Fatalf("read assistant conversation before turn: %v", err)
	}
	turn, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-turn-1"}, Payload: command.AssistantTurnInput{
			ConversationRef: created.Conversation.Ref, Content: "Create a sales project",
		}})
	if err != nil || turn.Plan != nil {
		t.Fatalf("queue assistant turn without keyword fallback: plan=%#v err=%v", turn.Plan, err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-claim-1"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim assistant execution: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	planResult, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-plan-1"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Summary: "Create project Sales", Operations: []entity.AssistantPlanOperation{{Key: "operation-001", Type: "CREATE_PROJECT", Summary: "Create sales project",
				Input: map[string]any{"name": "Sales", "purpose": "Qualify and convert leads", "language": "en"}}},
		}})
	if err != nil || planResult.Plan == nil || planResult.Plan.State != "PROPOSED" {
		t.Fatalf("propose assistant plan: result=%#v err=%v", planResult.Plan, err)
	}
	expectedPlanVersion := int64(1)
	applied, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-apply-1", ExpectedVersion: &expectedPlanVersion},
		Payload:  command.AssistantPlanInput{PlanRef: planResult.Plan.Ref}})
	if err != nil || applied.Plan == nil || applied.Plan.State != "APPLIED" || len(applied.CreatedRefs) != 1 {
		t.Fatalf("apply assistant plan: result=%#v refs=%v err=%v", applied.Plan, applied.CreatedRefs, err)
	}
}

func resolvedTestPrincipal(t *testing.T, ctx context.Context, repository *Repository, input platformrepo.ProofPrincipalInput, workload string) value.Principal {
	t.Helper()
	authority, err := repository.ResolveProofAuthority(ctx, input)
	if err != nil {
		t.Fatalf("resolve test proof authority: %v", err)
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID,
		Permission: input.Operation, CorrelationRef: input.Operation + "-component", CallerWorkload: workload, CredentialRevision: 1}
}

func assertBootstrapReadback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var organizationCount, ownerContractCount, systemAssistantCount, corePromptCount int
	var assistantRuntimeCount, capabilityCount, integrationDefinitionCount int
	var providerDefinitionCount, providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount int
	if err := pool.QueryRow(ctx, bootstrapComponentReadbackQuery).Scan(
		&organizationCount, &ownerContractCount, &systemAssistantCount, &corePromptCount,
		&assistantRuntimeCount, &capabilityCount, &integrationDefinitionCount,
		&providerDefinitionCount, &providerAccountCount, &providerCredentialRevisionCount,
		&completedBootstrapCount,
	); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if organizationCount != 1 || ownerContractCount != 1 || systemAssistantCount != 1 ||
		corePromptCount != 1 || assistantRuntimeCount != 1 || capabilityCount != 8 ||
		integrationDefinitionCount != 3 || providerDefinitionCount != 1 || providerAccountCount != 1 ||
		providerCredentialRevisionCount != 1 || completedBootstrapCount != 1 {
		t.Fatalf("unexpected bootstrap state: organization=%d owner_contract=%d assistant=%d core_prompt=%d runtime=%d capabilities=%d integrations=%d provider_definitions=%d provider_accounts=%d provider_credentials=%d completed=%d",
			organizationCount, ownerContractCount, systemAssistantCount, corePromptCount,
			assistantRuntimeCount, capabilityCount, integrationDefinitionCount, providerDefinitionCount,
			providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount)
	}
}
