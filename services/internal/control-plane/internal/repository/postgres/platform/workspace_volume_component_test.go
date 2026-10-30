package platform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// claimPreparedWorkspaceComponent drives owner commands through pending CREATE/PREPARE to a
// model claim. Physical observations are explicit test fixtures, never evidence of Kubernetes work.
// claimPreparedWorkspaceComponent проводит owner commands через pending CREATE/PREPARE к
// model claim. Physical observations — явные fixtures, не доказательство Kubernetes work.
func claimPreparedWorkspaceComponent(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, worker value.Principal,
	workload string, execute func(command.Kind, value.Principal, string, any) command.Result) command.Result {
	t.Helper()
	principal := func(operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
			ExternalTenantID: "kodex-installation", CallerWorkload: "session-archive", Operation: operation}, "session-archive")
	}
	claimer := principal(command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks))
	completer := principal(command.WorkspaceVolumeCommandPermission(command.CompleteWorkspaceVolumeTask))
	renewer := principal(command.WorkspaceVolumeRenewPermission)
	for _, stage := range []struct{ kind, key string }{{runtimecontract.WorkspaceVolumeCreate, "create"}, {runtimecontract.WorkspaceVolumePrepare, "prepare"}} {
		pending := execute(command.ClaimExecution, worker, "volume-pending-"+stage.key, command.LeaseInput{WorkloadInstance: workload, Limit: 1})
		if len(pending.RuntimeItems) != 0 {
			t.Fatal("model execution was admitted before physical preparation")
		}
		if !t.Run("profile-off-retains-"+stage.key, func(t *testing.T) {
			previous := repository.roleImages.WorkspaceStorageProfile
			repository.roleImages.WorkspaceStorageProfile = ""
			defer func() { repository.roleImages.WorkspaceStorageProfile = previous }()
			blocked := execute(command.ClaimExecution, worker, "volume-profile-off-"+stage.key,
				command.LeaseInput{WorkloadInstance: workload, Limit: 1})
			if len(blocked.RuntimeItems) != 0 {
				t.Fatal("profile opt-out bypassed existing V2 custody")
			}
		}) {
			t.FailNow()
		}
		claimInput := command.WorkspaceVolumeClaimInput{WorkloadInstance: "workspace-volume-component", Limit: 1}
		claimed := execute(command.ClaimWorkspaceVolumeTasks, claimer, "volume-claim-"+stage.key, claimInput)
		if len(claimed.RuntimeItems) != 1 {
			t.Fatal("owner did not issue exactly one physical task")
		}
		item := claimed.RuntimeItems[0]
		work := workspaceVolumeComponentWork(t, item)
		if work.Kind != stage.kind || work.Attempt != 1 || work.ReconcileOnly {
			t.Fatal("first preparation has an unexpected immutable kind or attempt")
		}
		replay := execute(command.ClaimWorkspaceVolumeTasks, claimer, "volume-claim-"+stage.key, claimInput)
		if len(replay.RuntimeItems) != 1 || workspaceVolumeComponentWork(t, replay.RuntimeItems[0]) != work ||
			stringMap(replay.RuntimeItems[0], "leaseRef") != stringMap(item, "leaseRef") ||
			stringMap(replay.RuntimeItems[0], "fence") != stringMap(item, "fence") {
			t.Fatal("physical claim replay issued a different task or authority")
		}
		proof := command.WorkspaceVolumeTaskInput{TaskRef: work.TaskRef, LeaseRef: stringMap(item, "leaseRef"),
			Fence: stringMap(item, "fence"), Generation: runtimeRevisionMapInt64(item, "generation")}
		renewed, err := service.RenewWorkspaceVolumeTask(ctx, renewer, proof)
		if err != nil || stringMap(renewed, "leaseRef") != proof.LeaseRef || stringMap(renewed, "fence") != proof.Fence ||
			runtimeRevisionMapInt64(renewed, "generation") != proof.Generation {
			t.Fatal("physical grant renewal changed its exact tuple or failed")
		}
		report := proof
		report.ObservedPVCUID = "00000000-0000-4000-8000-000000000010"
		if stage.kind == runtimecontract.WorkspaceVolumePrepare {
			report.JobUID, report.PodUID = "00000000-0000-4000-8000-000000000011", "00000000-0000-4000-8000-000000000012"
			report.Ready = &runtimecontract.WorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: 1, Inode: 2,
				Preparation: runtimecontract.WorkspaceVolumePreparation{Scope: work.Volume.Scope, WorkspaceRef: work.Volume.WorkspaceRef,
					TaskRef: work.Volume.PreparationTaskRef, PVCUID: work.Volume.PVCUID, VolumeGeneration: work.Volume.VolumeGeneration}}
		}
		execute(command.CompleteWorkspaceVolumeTask, completer, "volume-complete-"+stage.key, report)
		execute(command.CompleteWorkspaceVolumeTask, completer, "volume-complete-"+stage.key, report)
		if _, err := service.RenewWorkspaceVolumeTask(ctx, renewer, proof); err == nil {
			t.Fatal("completed physical grant was renewed")
		}
	}
	return execute(command.ClaimExecution, worker, "claim", command.LeaseInput{WorkloadInstance: workload, Limit: 1})
}

// workspaceVolumeComponentWork decodes the actual owner response for both fresh and durable
// replay shapes, checking its digest before a disposable observation is reported.
// workspaceVolumeComponentWork декодирует actual owner response в fresh и durable replay
// формах, проверяя digest до передачи disposable observation.
func workspaceVolumeComponentWork(t *testing.T, item map[string]any) runtimecontract.WorkspaceVolumeWork {
	t.Helper()
	raw, err := json.Marshal(item["work"])
	var work runtimecontract.WorkspaceVolumeWork
	if err != nil || runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &work) != nil || work.Validate() != nil {
		t.Fatal("physical owner response lost its immutable work commitment")
	}
	return work
}

// testWorkspaceVolumeCancellationOwner retains a late positive CREATE fact after real owner
// cancellation without reopening the origin, physical grant, READY state or model admission.
// testWorkspaceVolumeCancellationOwner сохраняет late positive CREATE fact после настоящей
// owner cancellation без открытия origin, physical grant, READY state либо model admission.
func testWorkspaceVolumeCancellationOwner(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service,
	owner, worker value.Principal, projectRef, agentRef string) {
	t.Helper()
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "workspace-volume-cancel-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("volume cancellation command %s failed: %v", key, err)
		}
		return result
	}
	run := execute(command.LaunchRun, owner, "launch", command.LaunchRunInput{ProjectRef: projectRef,
		Title: "Остановить подготовку каталога", Task: "Сохранить позднее наблюдение без нового запуска",
		Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}}).Run
	if run == nil {
		t.Fatal("volume cancellation origin was not created")
	}
	if items := execute(command.ClaimExecution, worker, "pending", command.LeaseInput{WorkloadInstance: "workspace-volume-cancel", Limit: 1}).RuntimeItems; len(items) != 0 {
		t.Fatal("cancellation fixture admitted a model before physical preparation")
	}
	principal := func(operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
			ExternalTenantID: "kodex-installation", CallerWorkload: "session-archive", Operation: operation}, "session-archive")
	}
	claimer := principal(command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks))
	completer := principal(command.WorkspaceVolumeCommandPermission(command.CompleteWorkspaceVolumeTask))
	renewer := principal(command.WorkspaceVolumeRenewPermission)
	claimInput := command.WorkspaceVolumeClaimInput{WorkloadInstance: "workspace-volume-cancel", Limit: 1}
	claimed := execute(command.ClaimWorkspaceVolumeTasks, claimer, "claim", claimInput)
	if len(claimed.RuntimeItems) != 1 {
		t.Fatal("cancellation fixture did not receive one physical task")
	}
	item := claimed.RuntimeItems[0]
	work := workspaceVolumeComponentWork(t, item)
	if work.Kind != runtimecontract.WorkspaceVolumeCreate || work.Volume.Scope.SessionRef != run.SessionRef {
		t.Fatal("cancellation fixture borrowed another physical origin")
	}
	proof := command.WorkspaceVolumeTaskInput{TaskRef: work.TaskRef, LeaseRef: stringMap(item, "leaseRef"),
		Fence: stringMap(item, "fence"), Generation: runtimeRevisionMapInt64(item, "generation")}
	currentRun, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal("read current physical origin before cancellation", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workspace-volume-cancel-cancel", ExpectedVersion: &currentRun.Version},
		Payload:  command.RunCommandInput{RunRef: run.Ref}}); err != nil {
		t.Fatal("cancel current physical origin", err)
	}
	if _, err := service.RenewWorkspaceVolumeTask(ctx, renewer, proof); err == nil {
		t.Fatal("cancellation left the physical grant renewable")
	}
	if pending := execute(command.ClaimWorkspaceVolumeTasks, claimer, "after-cancel", claimInput); len(pending.RuntimeItems) != 0 {
		t.Fatal("cancellation issued a replacement physical attempt")
	}
	report := proof
	report.ObservedPVCUID = "00000000-0000-4000-8000-000000000020"
	for attempt := 0; attempt < 2; attempt++ {
		result := execute(command.CompleteWorkspaceVolumeTask, completer, "late-fact", report)
		if result.Runtime["pastFact"] != true || stringMap(result.Runtime, "recordedTaskState") != "CANCELLED" ||
			stringMap(result.Runtime, "recordedVolumeState") == "READY" {
			t.Fatal("late physical fact reopened cancelled execution authority")
		}
	}
	terminal, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil || terminal.State != "CANCELLED" {
		t.Fatal("late physical report changed the terminal business outcome")
	}
	if pending := execute(command.ClaimExecution, worker, "model-after-cancel", command.LeaseInput{WorkloadInstance: "workspace-volume-cancel", Limit: 1}); len(pending.RuntimeItems) != 0 {
		t.Fatal("late physical report revived model admission")
	}
}
