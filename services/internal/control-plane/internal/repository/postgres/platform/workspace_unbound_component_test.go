package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestWorkspaceUnboundStopOwnerComponent proves a real owner retires only the closed unbound
// initial intent, preserves the prepared workspace and admits a fresh explicitly selected turn.
// TestWorkspaceUnboundStopOwnerComponent доказывает закрытие owner только closed unbound
// initial intent, сохранение prepared workspace и допуск нового явно выбранного хода.
func TestWorkspaceUnboundStopOwnerComponent(t *testing.T) {
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
	if err := repository.Ready(ctx); err != nil {
		t.Fatal("workspace owner schema is not ready", err)
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
	granted, grantErr := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workspace-owner-artifact", ExpectedVersion: &agent.Version},
		Payload:  command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}})
	if grantErr != nil || granted.Agent == nil {
		t.Fatal("grant explicit workspace authority", grantErr)
	}
	agent = *granted.Agent
	target := entity.RunTarget{Type: "AGENT", Ref: agent.Ref}
	run := execute(command.LaunchRun, owner, "launch", command.LaunchRunInput{ProjectRef: project.Ref,
		Title: "Сохранить рабочие исходники", Task: "Подготовить небольшой сервис", Target: target}).Run
	if run == nil {
		t.Fatal("workspace run was not returned")
	}
	claimed := claimPreparedWorkspaceComponent(t, ctx, repository, service, worker, "workspace-owner-controller", run.Ref, execute)
	if len(claimed.RuntimeItems) != 1 {
		t.Fatalf("expected one V2 execution claim, got %d", len(claimed.RuntimeItems))
	}
	item := claimed.RuntimeItems[0]
	if !slices.Contains(runtimeRevisionStringSlice(item["capabilities"]), runtimecontract.ArtifactCapability) {
		t.Fatal("runtime revision lost the owner's artifact capability for its own workspace")
	}
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

	payload, record := unboundStopForClaimFixture(t, item, binding)
	if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer, Mutation: value.Mutation{IdempotencyKey: "unbound-active"}, Payload: payload}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("active lease was retired", err)
	}
	current, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "workspace-owner-cancel-unbound", ExpectedVersion: &current.Version}, Payload: command.RunCommandInput{RunRef: current.Ref, Reason: "Synthetic cold-start failure before BIND"}}); err != nil {
		t.Fatal("close exact cold-start attempt", err)
	}
	for index, mutate := range []func(*command.WorkspaceObservationInput){
		func(v *command.WorkspaceObservationInput) { v.Fence = "synthetic-wrong-fence" },
		func(v *command.WorkspaceObservationInput) { v.WorkspaceGeneration++ },
		func(v *command.WorkspaceObservationInput) { v.ExecutionBindingDigest = strings.Repeat("d", 64) },
		func(v *command.WorkspaceObservationInput) { v.UnboundStop = nil },
		func(v *command.WorkspaceObservationInput) { v.Phase = runtimecontract.WorkspaceObserveBind },
	} {
		changed := payload
		mutate(&changed)
		if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer, Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("unbound-invalid-%d", index)}, Payload: changed}); err == nil {
			t.Fatal("invalid closed intent mutated workspace")
		}
	}
	testUnboundReceiptSQLSpoof(t, ctx, pool, payload)
	retired := execute(command.ObserveWorkspace, observer, "stop-unbound", payload)
	if stringMap(retired.Runtime, "workspaceUnboundStop") != string(payload.UnboundStop) || stringMap(retired.Runtime, "workspaceWriterAck") != "" {
		t.Fatal("unbound stop invented a writer ACK or lost its receipt")
	}
	replay := execute(command.ObserveWorkspace, observer, "stop-unbound", payload)
	if stringMap(replay.Runtime, "workspaceUnboundStop") != string(payload.UnboundStop) {
		t.Fatal("unbound receipt replay changed")
	}
	t.Run("immutable_receipt_acl", func(t *testing.T) {
		for _, statement := range []string{"UPDATE control_plane.workspace_unbound_stops SET record=record", "DELETE FROM control_plane.workspace_unbound_stops", "TRUNCATE control_plane.workspace_unbound_stops"} {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, mutationErr := tx.Exec(ctx, statement)
			_ = tx.Rollback(ctx)
			var denied *pgconn.PgError
			if !errors.As(mutationErr, &denied) || denied.Code != "42501" {
				t.Fatal("runtime changed immutable receipt", mutationErr)
			}
		}
	})
	var nativeRows int
	var state string
	var noWriter bool
	if err := pool.QueryRow(ctx, `SELECT w.state,w.writer_lease_id IS NULL,(SELECT count(*) FROM control_plane.session_storage native WHERE native.session_id=w.session_id) FROM control_plane.session_workspace_storage w WHERE w.ref=$1`, binding.WorkspaceRef).Scan(&state, &noWriter, &nativeRows); err != nil || state != "EMPTY" || !noWriter || nativeRows != 0 {
		t.Fatal("empty initial intent was not safely retired", err)
	}
	closed, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil || closed.State != "CANCELLED" {
		t.Fatal("stopped fact revived closed business run", err)
	}
	bind := payload
	bind.Phase = runtimecontract.WorkspaceObserveBind
	bind.UnboundStop = nil
	bind.PodUID = record.Execution.PodUID
	bind.PVCUID = record.Execution.PVCUID
	if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer, Mutation: value.Mutation{IdempotencyKey: "unbound-late-bind"}, Payload: bind}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("old kubelet acquired BIND after retirement", err)
	}
	next := execute(command.AddSessionTurn, owner, "explicit-after-unbound", command.SessionTurnInput{SessionRef: run.SessionRef, RunRef: run.Ref, Task: "Explicit owner-selected retry after verified stop"})
	if next.Run == nil {
		t.Fatal("explicit owner retry did not create a run")
	}
	claimedAgain := execute(command.ClaimExecution, worker, "claim-after-unbound", command.LeaseInput{WorkloadInstance: "workspace-owner-controller", Limit: 1})
	if len(claimedAgain.RuntimeItems) != 1 {
		t.Fatal("stopped intent prevented a fresh eligible claim")
	}
	fresh := claimedAgain.RuntimeItems[0]
	if stringMap(fresh, "leaseRef") == payload.LeaseRef || stringMap(fresh, "runtimeRevisionRef") == record.Execution.RuntimeRevisionRef || fresh["nativeSession"] != nil {
		t.Fatal("new claim reused old authority or invented a native checkpoint")
	}
	freshBinding, err := runtimeWorkspaceSnapshotBinding(fresh)
	if err != nil || freshBinding.WorkspaceRef != binding.WorkspaceRef || freshBinding.Generation != binding.Generation {
		t.Fatal("unbound stop discarded the prepared workspace", err)
	}
	read := payload
	read.UnboundStop = nil
	rejoined := execute(command.ObserveWorkspace, observer, "read-unbound", read)
	if stringMap(rejoined.Runtime, "workspaceUnboundStop") != string(payload.UnboundStop) {
		t.Fatal("lost ACK did not rejoin exact stopped receipt")
	}
	oldAgain := execute(command.ObserveWorkspace, observer, "stop-unbound", payload)
	_ = oldAgain
	var currentLease string
	if err := pool.QueryRow(ctx, `SELECT lease.ref FROM control_plane.session_workspace_storage workspace JOIN control_plane.runtime_leases lease ON lease.id=workspace.writer_lease_id WHERE workspace.ref=$1`, binding.WorkspaceRef).Scan(&currentLease); err != nil || currentLease != stringMap(fresh, "leaseRef") {
		t.Fatal("historical stop receipt cleared the new writer", err)
	}
}

// unboundStopForClaimFixture creates synthetic controller pins for a real owner-issued claim.
// unboundStopForClaimFixture создаёт synthetic controller pins для настоящего owner-issued claim.
func unboundStopForClaimFixture(t *testing.T, item map[string]any, binding *runtimecontract.WorkspaceStorageBinding) (command.WorkspaceObservationInput, runtimecontract.WorkspaceUnboundStop) {
	t.Helper()
	record := runtimecontract.WorkspaceUnboundStop{Schema: runtimecontract.WorkspaceUnboundStopSchema, Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation, NodeName: "synthetic-node", NodeUID: "00000000-0000-4000-8000-000000000020", Evidence: "FIRST_INIT_FAILED", Execution: runtimecontract.WorkspaceWriterCommitment{
		RunRef: stringMap(item, "runRef"), NodeRef: stringMap(item, "nodeRef"), TurnRef: stringMap(item, "turnRef"), RuntimeRevisionRef: stringMap(item, "runtimeRevisionRef"), RuntimeRevisionVersion: runtimeRevisionMapInt64(item, "generation"), RuntimeRevisionDigest: stringMap(item, "revisionDigest"), ExecutionBindingDigest: strings.Repeat("c", 64), InputDigest: stringMap(item, "inputDigest"), LeaseRef: stringMap(item, "leaseRef"), LeaseGeneration: runtimeRevisionMapInt64(item, "generation"), Attempt: int32(runtimeRevisionMapInt64(item, "attempt")), PodUID: "00000000-0000-4000-8000-000000000009", PVCUID: binding.VolumeReady.Preparation.PVCUID}}
	raw, err := runtimecontract.EncodeWorkspaceUnboundStop(record)
	if err != nil {
		t.Fatal(err)
	}
	return command.WorkspaceObservationInput{LeaseRef: record.Execution.LeaseRef, Fence: stringMap(item, "fence"), Generation: record.Execution.LeaseGeneration, RuntimeRevisionDigest: record.Execution.RuntimeRevisionDigest, ExecutionBindingDigest: record.Execution.ExecutionBindingDigest, WorkspaceRef: binding.WorkspaceRef, WorkspaceGeneration: binding.Generation, Phase: runtimecontract.WorkspaceObserveUnboundStop, UnboundStop: raw}, record
}

// testWorkspaceUnboundResumeOwner proves a failed pre-BIND resume keeps the complete verified
// native predecessor and restored files, while an explicit new turn obtains fresh authority.
// testWorkspaceUnboundResumeOwner доказывает сохранение полного verified native predecessor и
// restored files после failed pre-BIND resume; explicit new turn получает свежие полномочия.
func testWorkspaceUnboundResumeOwner(t *testing.T, ctx context.Context, service *platformservice.Service, pool *pgxpool.Pool,
	owner, worker, observer value.Principal, claimed command.Result, run *entity.Run, binding *runtimecontract.WorkspaceStorageBinding,
	execute func(command.Kind, value.Principal, string, any) command.Result,
) (command.Result, *entity.Run) {
	t.Helper()
	readNative := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT (to_jsonb(native)-ARRAY['native_writer_lease_ref','native_writer_revision_id','version','updated_at'])::text
  FROM control_plane.session_storage native JOIN control_plane.sessions session ON session.id=native.session_id WHERE session.ref=$1`, run.SessionRef).Scan(&snapshot); err != nil {
			t.Fatal("read verified native predecessor", err)
		}
		return snapshot
	}
	before := readNative()
	current, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "unbound-resume-cancel", ExpectedVersion: &current.Version}, Payload: command.RunCommandInput{RunRef: run.Ref}}); err != nil {
		t.Fatal("close unbound resume", err)
	}
	stop, _ := unboundStopForClaimFixture(t, claimed.RuntimeItems[0], binding)
	result := execute(command.ObserveWorkspace, observer, "unbound-resume-stop", stop)
	if stringMap(result.Runtime, "workspaceStorageState") != "RESTORED" || readNative() != before {
		t.Fatal("unbound resume lost restored tree or native predecessor")
	}
	var noWriter bool
	if err := pool.QueryRow(ctx, `SELECT native.native_writer_lease_ref IS NULL AND native.native_writer_revision_id IS NULL
 FROM control_plane.session_storage native JOIN control_plane.sessions session ON session.id=native.session_id WHERE session.ref=$1`, run.SessionRef).Scan(&noWriter); err != nil || !noWriter {
		t.Fatal("unbound resume retained stale native intent", err)
	}
	var next command.Result
	if run.Target.Type == "WORKFLOW" {
		next = execute(command.LaunchRun, owner, "unbound-resume-explicit-workflow", command.LaunchRunInput{ProjectRef: binding.Scope.ProjectRef, SessionRef: run.SessionRef, Title: "Явный повтор процесса", Task: "Explicit retry retaining verified predecessor", Target: run.Target})
	} else {
		next = execute(command.AddSessionTurn, owner, "unbound-resume-explicit", command.SessionTurnInput{SessionRef: run.SessionRef, RunRef: run.Ref, Task: "Explicit retry retaining verified predecessor"})
	}
	if next.Run == nil {
		t.Fatal("explicit resume retry missing run")
	}
	fresh := execute(command.ClaimExecution, worker, "unbound-resume-claim", command.LeaseInput{WorkloadInstance: "workspace-restore-controller", Limit: 1})
	if len(fresh.RuntimeItems) != 1 || fresh.RuntimeItems[0]["nativeSession"] == nil || readNative() != before {
		t.Fatal("fresh resume did not retain exact native history")
	}
	currentBinding, err := runtimeWorkspaceSnapshotBinding(fresh.RuntimeItems[0])
	if err != nil || currentBinding == nil || currentBinding.Generation != binding.Generation || currentBinding.Source == nil || *currentBinding.Source != *binding.Source {
		t.Fatal("fresh resume changed restored generation or source", err)
	}
	return fresh, next.Run
}

// testUnboundReceiptSQLSpoof exercises the runtime SQL boundary independently of RPC decoding;
// malformed/foreign receipts and archive attachment cannot open the narrow retirement transition.
// testUnboundReceiptSQLSpoof проверяет runtime SQL boundary независимо от RPC decoder;
// malformed/foreign receipts и archive attachment не открывают узкий retirement transition.
func testUnboundReceiptSQLSpoof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, payload command.WorkspaceObservationInput) {
	t.Helper()
	insert := `INSERT INTO control_plane.workspace_unbound_stops(organization_id,workspace_id,workspace_generation,lease_id,previous_state,record)
 SELECT workspace.organization_id,workspace.id,workspace.generation,lease.id,'EMPTY',$3
 FROM control_plane.session_workspace_storage workspace JOIN control_plane.runtime_leases lease ON lease.ref=$2 AND lease.organization_id=workspace.organization_id
 WHERE workspace.ref=$1`
	for _, variant := range []string{"scope", "run", "node", "turn", "revision-version", "attempt", "lease-generation", "pod", "pvc", "join", "binding", "node-uid", "evidence", "unknown-field", "duplicate-key", "attach-archive"} {
		t.Run("sql-"+variant, func(t *testing.T) {
			var value map[string]any
			if json.Unmarshal(payload.UnboundStop, &value) != nil {
				t.Fatal("decode synthetic record")
			}
			execution := value["execution"].(map[string]any)
			switch variant {
			case "scope":
				value["scope"].(map[string]any)["project_ref"] = "prj_other000"
			case "run":
				execution["run_ref"] = "run_other000"
			case "node":
				execution["node_ref"] = "nod_other000"
			case "turn":
				execution["turn_ref"] = "trn_other000"
			case "revision-version":
				execution["runtime_revision_version"] = 99
			case "attempt":
				execution["attempt"] = 99
			case "lease-generation":
				execution["lease_generation"] = 99
			case "pod":
				execution["pod_uid"] = "../foreign"
			case "pvc":
				execution["pvc_uid"] = "40000000-0000-4000-8000-000000000001"
			case "join":
				execution["join_sha256"] = strings.Repeat("a", 64)
			case "binding":
				execution["execution_binding_digest"] = "unknown"
			case "node-uid":
				value["node_uid"] = "../foreign"
			case "evidence":
				value["evidence"] = "UNKNOWN"
			case "unknown-field":
				value["unknown"] = true
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "duplicate-key" {
				raw = bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"kodex.runtime-workspace-unbound-stop.v1","schema":`), 1)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			_, mutationErr := tx.Exec(ctx, insert, payload.WorkspaceRef, payload.LeaseRef, string(raw))
			expected := "unbound stop receipt scope is invalid"
			if variant == "attach-archive" {
				if mutationErr != nil {
					t.Fatal("exact synthetic receipt rejected", mutationErr)
				}
				_, mutationErr = tx.Exec(ctx, `UPDATE control_plane.session_workspace_storage SET state='EMPTY',writer_lease_id=NULL,current_archive_id=gen_random_uuid(),version=version+1 WHERE ref=$1`, payload.WorkspaceRef)
				expected = "session workspace transition is invalid"
			}
			var denied *pgconn.PgError
			if !errors.As(mutationErr, &denied) || denied.Code != "P0001" || denied.Message != expected {
				t.Fatal("SQL spoof was not rejected by exact guard", mutationErr)
			}
		})
	}
}
