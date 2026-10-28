package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestWorkspaceWriterOwnerComponent uses real owner commands and immutable RR/lease rows for V2 writer custody.
// The physical Pod/PVC observations are disposable controller fixtures; no real model, cluster or object store is invoked.
// TestWorkspaceWriterOwnerComponent использует настоящие owner commands и immutable RR/lease rows для V2 writer custody.
// Physical Pod/PVC observations — disposable controller fixtures; настоящая модель, кластер и object store не вызываются.
func TestWorkspaceWriterOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open workspace owner fixture", err)
	}
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	configuration := repository.roleImages
	configuration.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	configuration.WorkspaceStorageProfile = runtimecontract.WorkspaceStorageProfile
	if err := repository.ConfigureRoleImages(configuration); err != nil {
		t.Fatal("configure exact workspace ABI", err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap workspace owner fixture", err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal("construct workspace owner service", err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
		ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	observer := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
		ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: runtimecontract.WorkspaceObservationOperation}, "runtime-controller")
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "workspace-owner-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("workspace owner command %s failed: %v", key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Устойчивый рабочий каталог", Language: "ru"}).Project
	if project == nil {
		t.Fatal("workspace project was not returned")
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "workspace-owner-agent", "Разработчик")
	run := execute(command.LaunchRun, owner, "launch", command.LaunchRunInput{ProjectRef: project.Ref,
		Title: "Сохранить рабочие исходники", Task: "Подготовить небольшой сервис", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	if run == nil {
		t.Fatal("workspace run was not returned")
	}
	claimed := claimPreparedWorkspaceComponent(t, ctx, repository, service, worker, "workspace-owner-controller", execute)
	if len(claimed.RuntimeItems) != 1 {
		t.Fatalf("expected one V2 execution claim, got %d", len(claimed.RuntimeItems))
	}
	item := claimed.RuntimeItems[0]
	binding, err := runtimeWorkspaceSnapshotBinding(item)
	if err != nil || binding == nil {
		t.Fatal("owner did not assign workspace binding", err)
	}
	if !t.Run("profile_off_mismatched_workspace_rejected", func(t *testing.T) {
		testWorkspaceProfileOffRejectsMismatchedRow(t, ctx, repository, service, owner, project.Ref, binding.WorkspaceRef)
	}) {
		return
	}
	if binding.Generation != 1 || binding.Scope.AgentRef != agent.Ref || binding.Scope.SessionRef != run.SessionRef {
		t.Fatal("workspace binding does not match assigned employee session")
	}
	if binding.VolumeReady == nil || binding.VolumeReady.Preparation.PVCUID != "00000000-0000-4000-8000-000000000010" {
		t.Fatal("model snapshot omitted its accepted physical preparation")
	}
	payload := command.WorkspaceObservationInput{LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: runtimeRevisionMapInt64(item, "generation"),
		RuntimeRevisionDigest: stringMap(item, "revisionDigest"), ExecutionBindingDigest: strings.Repeat("c", 64),
		WorkspaceRef: binding.WorkspaceRef, WorkspaceGeneration: binding.Generation, Phase: runtimecontract.WorkspaceObserveBind,
		PodUID: "00000000-0000-4000-8000-000000000009", PVCUID: "00000000-0000-4000-8000-000000000010"}
	if !t.Run("fresh_bind", func(t *testing.T) {
		result := execute(command.ObserveWorkspace, observer, "bind", payload)
		if stringMap(result.Runtime, "workspaceStorageState") != "WRITING" || stringMap(result.Runtime, "workspaceWriterAck") == "" {
			t.Fatal("owner did not acknowledge current writer")
		}
	}) {
		return
	}
	bound := execute(command.ObserveWorkspace, observer, "bind", payload)
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement([]byte(stringMap(bound.Runtime, "workspaceWriterAck")))
	if err != nil || ack.Scope != binding.Scope || ack.Generation != binding.Generation {
		t.Fatal("bound writer receipt lost exact scope", err)
	}
	t.Run("foreign_writer_rejected", func(t *testing.T) {
		foreign := payload
		foreign.WorkspaceRef = "wks_foreign000"
		if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer,
			Mutation: value.Mutation{IdempotencyKey: "workspace-owner-foreign"}, Payload: foreign}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign workspace was not rejected", err)
		}
	})
	currentRun, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal("read running workspace run", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workspace-owner-cancel", ExpectedVersion: &currentRun.Version},
		Payload:  command.RunCommandInput{RunRef: run.Ref}}); err != nil {
		t.Fatal("cancel workspace run", err)
	}
	t.Run("terminal_bind_replay_rejected", func(t *testing.T) {
		if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer,
			Mutation: value.Mutation{IdempotencyKey: "workspace-owner-bind"}, Payload: payload}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("terminal bind receipt restored authority", err)
		}
	})
	blocked := payload
	blocked.Phase, blocked.BlockReason = runtimecontract.WorkspaceObserveBlocked, "POD_MISSING"
	blocked.PodUID, blocked.PVCUID = "", ""
	blockResult := execute(command.ObserveWorkspace, observer, "blocked", blocked)
	if stringMap(blockResult.Runtime, "workspaceStorageState") != "BLOCKED" {
		t.Fatal("unknown writer was not retained")
	}
	released := blocked
	released.Phase, released.BlockReason = runtimecontract.WorkspaceObserveReleased, ""
	t.Run("missing_join_blocks_release", func(t *testing.T) {
		if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer,
			Mutation: value.Mutation{IdempotencyKey: "workspace-owner-no-join"}, Payload: released}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("missing join did not return closed authority failure", err)
		}
		var state string
		var released, joined bool
		if err := pool.QueryRow(ctx, `SELECT state,writer_released,quiescence_record IS NOT NULL
			FROM control_plane.session_workspace_storage WHERE ref=$1`, binding.WorkspaceRef).Scan(&state, &released, &joined); err != nil ||
			state != "BLOCKED" || released || joined {
			t.Fatal("rejected release changed writer custody", err)
		}
	})
	joined, err := runtimecontract.EncodeWorkspaceQuiescence(runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema,
		Scope: ack.Scope, WorkspaceRef: ack.WorkspaceRef, Generation: ack.Generation, Writer: ack.Writer, Joined: true})
	if err != nil {
		t.Fatal("encode positive controller join fixture", err)
	}
	quiescent := payload
	quiescent.Phase, quiescent.Quiescence = runtimecontract.WorkspaceObserveQuiescent, joined
	if !t.Run("late_join_preserves_terminal", func(t *testing.T) {
		result := execute(command.ObserveWorkspace, observer, "quiescent", quiescent)
		if stringMap(result.Runtime, "workspaceStorageState") != "CHECKPOINT_PENDING" {
			t.Fatal("matching late join was not accepted")
		}
	}) {
		return
	}
	claimPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.claim")
	t.Run("checkpoint_requires_released_writer", func(t *testing.T) {
		items, err := service.ClaimSessionArchiveTasks(ctx, claimPrincipal, "workspace-checkpoint-fixture", 1)
		if err != nil || len(items) != 0 {
			t.Fatal("checkpoint claimed before physical release acknowledgement", err)
		}
	})
	t.Run("released_writer_and_immutable_block_receipt", func(t *testing.T) {
		result := execute(command.ObserveWorkspace, observer, "released", released)
		if result.Runtime["workspaceWriterReleased"] != true || stringMap(result.Runtime, "workspaceStorageState") != "CHECKPOINT_PENDING" {
			t.Fatal("positive released writer was not recorded")
		}
		replayed := execute(command.ObserveWorkspace, observer, "blocked", blocked)
		if stringMap(replayed.Runtime, "workspaceCommittedPhase") != runtimecontract.WorkspaceObserveBlocked ||
			stringMap(replayed.Runtime, "workspaceStorageState") != "CHECKPOINT_PENDING" || replayed.Runtime["workspaceWriterReleased"] != true {
			t.Fatal("historical receipt was rewritten or current tuple lost")
		}
		terminal, err := service.GetRun(ctx, owner, run.Ref)
		if err != nil || terminal.State != "CANCELLED" {
			t.Fatal("storage-only observation reopened cancelled run", err)
		}
	})
	testWorkspaceCheckpointOwner(t, ctx, repository, service, pool, owner, claimPrincipal, binding, run.Ref)
	t.Run("physical_cancel_and_past_fact", func(t *testing.T) {
		testWorkspaceVolumeCancellationOwner(t, ctx, repository, service, owner, worker, project.Ref, agent.Ref)
	})
}
