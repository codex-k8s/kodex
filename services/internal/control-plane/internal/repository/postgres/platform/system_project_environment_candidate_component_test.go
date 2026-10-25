package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeEnvironmentCandidateIdentity reads exact native bootstrap identities for the row-lock-only fixture.
// runtimeEnvironmentCandidateIdentity читает точные native bootstrap identities для оснастки только с row locks.
//
//go:embed testdata/sql/runtime_environment_candidate_identity.sql
var runtimeEnvironmentCandidateIdentity string

// runtimeEnvironmentCandidateHoldBinding creates controlled row contention without changing binding metadata.
// runtimeEnvironmentCandidateHoldBinding создаёт управляемую row contention без изменения binding metadata.
//
//go:embed testdata/sql/runtime_environment_candidate_hold_binding.sql
var runtimeEnvironmentCandidateHoldBinding string

// runtimeEnvironmentCandidateProbePartialLocks proves that failed guard savepoint rollback released ENV and agent row locks while its outer transaction remains open.
// runtimeEnvironmentCandidateProbePartialLocks доказывает освобождение ENV и agent row locks после rollback неуспешного guard savepoint при открытой внешней transaction.
//
//go:embed testdata/sql/runtime_environment_candidate_probe_partial_locks.sql
var runtimeEnvironmentCandidateProbePartialLocks string

// testSystemProjectEnvironmentCandidate exercises two native SYSTEM turns with nonempty run project context and organization-owned agent/ENV metadata; no model/provider call or business-state SQL is used.
// testSystemProjectEnvironmentCandidate проверяет два штатных SYSTEM turn с непустым проектным контекстом запуска и общими agent/ENV metadata; model/provider calls и business-state SQL не используются.
func testSystemProjectEnvironmentCandidate(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, worker value.Principal) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal("read native SYSTEM assistant")
	}
	warm := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report",
	}, "runtime-controller")
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal("report native SYSTEM warm readiness")
	}
	execute := func(kind command.Kind, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "secret-retention-system-context-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("native SYSTEM context %s: %v", key, err)
		}
		return result
	}
	project := execute(command.CreateProject, "project", command.ProjectInput{Name: "SYSTEM environment project context", Language: "en"}).Project
	conversation := execute(command.CreateAssistantConversation, "conversation", command.AssistantConversationInput{AssistantScope: "SYSTEM", ProjectRef: project.Ref}).Conversation
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil || configuration.Environment.ScopeKind != "ORGANIZATION" || configuration.Environment.ProjectRef != "" ||
		conversation.AssistantScope != "SYSTEM" || conversation.ProjectRef != project.Ref {
		t.Fatal("native SYSTEM project context lost organization ENV ownership")
	}
	t.Run("binding-contention-releases-partial-locks", func(t *testing.T) {
		candidate := claimableExecution{projectID: gateTestProjectID(t, ctx, repository, owner, project.Ref)}
		var organizationID string
		if err := repository.pool.QueryRow(ctx, runtimeEnvironmentCandidateIdentity, assistant.Ref, configuration.Environment.CurrentVersion.Ref).Scan(
			&candidate.agentID, &candidate.runtimeEnvironmentRef, &candidate.runtimeEnvironmentID, &candidate.runtimeEnvironmentDigest,
			&candidate.environmentBindingID, &candidate.environmentBindingRef, &candidate.environmentBindingVersion, &candidate.environmentBindingDigest, &organizationID); err != nil {
			t.Fatal("read native SYSTEM contention identities")
		}
		testRuntimeEnvironmentCandidateContention(t, ctx, repository.pool, organizationID, candidate)
	})
	for _, key := range []string{"first", "next"} {
		turn := execute(command.AddAssistantTurn, key+"-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Return a synthetic SYSTEM scope result.", DeliveryMode: "QUEUE"}).Conversation
		claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "secret-retention-system-context-" + key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "secret-retention-system-context-worker", Limit: 10}})
		if err != nil || len(claimed.RuntimeItems) != 1 {
			t.Fatalf("SYSTEM project-context %s claim lost eligible work: %v", key, err)
		}
		lease := claimed.RuntimeItems[0]
		if stringMap(lease, "sessionRef") != turn.SessionRef || stringMap(lease, "assistantScope") != "SYSTEM" || stringMap(lease, "agentRef") != assistant.Ref ||
			stringMap(lease, "projectRef") != project.Ref || stringMap(lease, "runtimeEnvironmentRef") != configuration.Environment.Ref ||
			stringMap(lease, "runtimeEnvironmentDigest") != configuration.Environment.CurrentVersion.Digest {
			t.Fatal("SYSTEM project-context claim substituted its exact organization ENV pin")
		}
		run, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
		if err != nil || run.ProjectRef != project.Ref || run.AssistantPin == nil || run.AssistantPin.Scope != "SYSTEM" || run.Target.Ref != assistant.Ref || run.State != "RUNNING" {
			t.Fatal("authoritative SYSTEM run lost its actual nonempty project context")
		}
		completeClaimedExecutionWithSummary(t, ctx, service, worker, lease, "secret-retention-system-context-"+key, false, "Synthetic SYSTEM scope result")
		run, err = service.GetRun(ctx, owner, run.Ref)
		if err != nil || run.State != "SUCCEEDED" {
			t.Fatal("native SYSTEM project-context completion was not terminal")
		}
	}
}

// rollbackEnvironmentCandidateFixture releases only fixture row locks with an independent bounded cleanup context.
// rollbackEnvironmentCandidateFixture освобождает только row locks оснастки с независимым bounded cleanup context.
func rollbackEnvironmentCandidateFixture(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Error("rollback candidate fixture row locks failed")
	}
}

// testRuntimeEnvironmentCandidateContention keeps the outer transaction open after a binding miss and checks NOWAIT ownership of the released partial ENV/agent locks from another transaction.
// testRuntimeEnvironmentCandidateContention оставляет внешнюю transaction открытой после binding miss и проверяет из другой transaction NOWAIT-владение освобождёнными частичными ENV/agent locks.
func testRuntimeEnvironmentCandidateContention(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID string, candidate claimableExecution) {
	t.Helper()
	positive, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin positive guard fixture")
	}
	defer rollbackEnvironmentCandidateFixture(t, positive)
	if eligible, err := retainRuntimeEnvironmentCandidate(ctx, positive, organizationID, candidate); err != nil || !eligible {
		t.Fatalf("exact native owner pin is not eligible before contention: %v", err)
	}
	if err := positive.Rollback(ctx); err != nil {
		t.Fatal("release positive guard fixture locks")
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin binding lock holder")
	}
	defer rollbackEnvironmentCandidateFixture(t, holder)
	var ref string
	if err := holder.QueryRow(ctx, runtimeEnvironmentCandidateHoldBinding, candidate.environmentBindingID).Scan(&ref); err != nil || ref != candidate.environmentBindingRef {
		t.Fatal("lock exact native binding")
	}
	outer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin candidate outer transaction")
	}
	defer rollbackEnvironmentCandidateFixture(t, outer)
	guardContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if eligible, err := retainRuntimeEnvironmentCandidate(guardContext, outer, organizationID, candidate); err != nil || eligible {
		t.Fatalf("contended binding must skip without storage error: %v", err)
	}
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin partial-lock probe")
	}
	defer rollbackEnvironmentCandidateFixture(t, probe)
	if err := probe.QueryRow(ctx, runtimeEnvironmentCandidateProbePartialLocks, candidate.runtimeEnvironmentRef, candidate.agentID, organizationID).Scan(&ref); err != nil {
		t.Fatal("guard miss retained partial ENV/agent locks beyond savepoint rollback")
	}
}
