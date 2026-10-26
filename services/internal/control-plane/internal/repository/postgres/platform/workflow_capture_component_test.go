package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// queryWorkflowCaptureFixture prepares an unclaimed anchor in a disposable database only;
	// it tests the capture boundary, not automatic DAG scheduling.
	// queryWorkflowCaptureFixture подготавливает незапущенный anchor только в одноразовой БД;
	// запрос проверяет capture boundary, а не автоматический планировщик DAG.
	//go:embed testdata/sql/workflow_capture_fixture.sql
	queryWorkflowCaptureFixture string
	// queryWorkflowCaptureAssign exercises the guarded planned-to-physical assignment before claim.
	// queryWorkflowCaptureAssign проверяет защищённое назначение planned-to-physical перед claim.
	//go:embed testdata/sql/workflow_capture_assign.sql
	queryWorkflowCaptureAssign string
	// queryWorkflowCaptureProof reads independent producer identities and the stored batch.
	// queryWorkflowCaptureProof читает независимые identity производителя и сохранённый batch.
	//go:embed testdata/sql/workflow_capture_proof.sql
	queryWorkflowCaptureProof string
	// queryWorkflowCaptureMutate attempts bounded forbidden changes in rollback-only test transactions.
	// queryWorkflowCaptureMutate пробует запрещённые изменения в тестовых транзакциях с обязательным откатом.
	//go:embed testdata/sql/workflow_capture_mutate.sql
	queryWorkflowCaptureMutate string
	// queryWorkflowCaptureDelete probes unauthorized deletion of the exact retained physical node.
	// queryWorkflowCaptureDelete проверяет отказ при удалении точного retained physical node без purge authority.
	//go:embed testdata/sql/workflow_capture_delete.sql
	queryWorkflowCaptureDelete string
	// queryWorkflowCapturePurgeProof verifies the native purge receipt and removal of its private context.
	// queryWorkflowCapturePurgeProof проверяет native purge receipt и очистку приватного контекста удаления.
	//go:embed testdata/sql/workflow_capture_purge_proof.sql
	queryWorkflowCapturePurgeProof string
)

// workflowCaptureProof keeps only non-secret provenance from the disposable execution rows.
// workflowCaptureProof хранит только несекретное происхождение из одноразовых execution rows.
type workflowCaptureProof struct {
	Capture                         map[string]any
	BusinessAttempt                 int32
	LeaseState, RevisionID, LeaseID string
	TurnID                          string
	RuntimeAttempt                  int32
	Generation, ArtifactCount       int64
}

// TestWorkflowCaptureComponent proves real claim/completion capture, explicit empty batches,
// exact immutable content pins, receipt replay and rejection of capture before completion.
// The isolated fixture assigns a planned node directly; this is not a scheduler/PDLC acceptance test.
// TestWorkflowCaptureComponent проверяет capture настоящих claim/completion, явно пустой batch,
// точные immutable content pins, receipt replay и отказ до completion. Изолированная оснастка
// назначает planned node напрямую; это не приёмка планировщика или PDLC.
func TestWorkflowCaptureComponent(t *testing.T) {
	for _, withArtifact := range []bool{false, true} {
		name := "empty"
		if withArtifact {
			name = "artifact"
		}
		t.Run(name, func(t *testing.T) { testWorkflowCaptureCompletion(t, withArtifact) })
	}
}

// testWorkflowCaptureCompletion keeps all fault probes in transactions that are always rolled back;
// actual artifact insertion and capture run together through the unchanged owner completion service.
// testWorkflowCaptureCompletion откатывает каждую fault probe; фактические artifact insertion и capture
// выполняются вместе через штатный owner completion service.
func testWorkflowCaptureCompletion(t *testing.T, withArtifact bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated workflow capture database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		actor := owner
		if kind == command.ClaimExecution || kind == command.CompleteExecution {
			actor = worker
		}
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "workflow-capture-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("workflow capture command failed: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Capture boundary", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "capture-agent", "Исполнитель")
	agent = *execute(command.ChangeAgentCapability, "artifact-capability", &agent.Version,
		command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "capture-manager", "Менеджер")
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Capture boundary", Purpose: "Проверить происхождение",
		CoordinatorAgentRef: manager.Ref, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "capture", Position: 1, Name: "Материал", AgentRef: agent.Ref,
			Instructions: "Создать проверяемый материал.", TimeoutSeconds: 900, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability},
			ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "capture.md", MediaType: "text/markdown"}}}}}
	legacy := draft
	legacy.Name, legacy.ExecutionMode = "Legacy capture boundary", "MANAGER_DRIVEN"
	legacy.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
	legacy.Steps[0].ResultRequirements = nil
	legacyWorkflow := execute(command.CreateWorkflow, "legacy-workflow", nil, command.WorkflowInput{ProjectRef: project.Ref,
		Name: legacy.Name, Purpose: legacy.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &legacy}).Workflow
	if _, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workflow-capture-legacy-validation", ExpectedVersion: &legacyWorkflow.Version},
		Payload:  command.WorkflowInput{Ref: legacyWorkflow.Ref}}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("legacy coordinator without delegation authority passed validation")
	}
	workflow := execute(command.CreateWorkflow, "workflow", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name,
		Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := execute(command.LaunchRun, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Сохранить материал.",
		Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	nodeRef := "nod_capture_boundary_fixture"
	var nodeID string
	if err := pool.QueryRow(ctx, queryWorkflowCaptureFixture, pgx.StrictNamedArgs{
		"run_ref": run.Ref, "workflow_ref": workflow.Ref, "node_ref": nodeRef,
	}).Scan(&nodeID); err != nil {
		t.Fatal("prepare unclaimed workflow capture anchor", err)
	}
	if tag, err := pool.Exec(ctx, queryWorkflowCaptureAssign, pgx.StrictNamedArgs{"node_ref": nodeRef}); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("assign workflow capture anchor", err)
	}
	items := execute(command.ClaimExecution, "claim", nil, command.LeaseInput{WorkloadInstance: "workflow-capture-fixture", Limit: 1}).RuntimeItems
	if len(items) != 1 || stringMap(items[0], "nodeRef") != nodeRef {
		t.Fatal("exact workflow capture claim missing")
	}
	claimed := items[0]
	proof := func() workflowCaptureProof {
		t.Helper()
		var result workflowCaptureProof
		if err := pool.QueryRow(ctx, queryWorkflowCaptureProof, pgx.StrictNamedArgs{"node_ref": nodeRef, "lease_ref": stringMap(claimed, "leaseRef")}).Scan(
			&result.Capture, &result.BusinessAttempt, &result.LeaseState, &result.RevisionID, &result.LeaseID, &result.TurnID,
			&result.RuntimeAttempt, &result.Generation, &result.ArtifactCount); err != nil {
			t.Fatal("read exact workflow capture proof", err)
		}
		return result
	}
	before := proof()
	if before.Capture != nil || before.BusinessAttempt != 1 || before.LeaseState != "CLAIMED" {
		t.Fatal("unexpected pre-completion capture state")
	}
	reject := func(mutation string, expected workflowCaptureProof) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated capture fault")
		}
		if mutation == "delete" {
			_, err = tx.Exec(ctx, queryWorkflowCaptureDelete, pgx.StrictNamedArgs{"node_ref": nodeRef})
		} else {
			_, err = tx.Exec(ctx, queryWorkflowCaptureMutate, pgx.StrictNamedArgs{"node_ref": nodeRef, "mutation": mutation,
				"revision_id": before.RevisionID, "lease_id": before.LeaseID, "generation": before.Generation})
		}
		var failure *pgconn.PgError
		closed := errors.As(err, &failure) && failure.Code == "23514"
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil {
			t.Fatal("rollback isolated capture fault")
		}
		if !closed || !reflect.DeepEqual(proof(), expected) {
			t.Fatalf("capture fault was not rejected atomically: %s", mutation)
		}
	}
	reject("empty-before-completion", before)
	reject("rewind", before)
	payload := command.CompleteExecutionInput{LeaseRef: stringMap(claimed, "leaseRef"), Fence: stringMap(claimed, "fence"),
		Generation: runtimeRevisionMapInt64(claimed, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: "Capture boundary."}
	body := []byte("Проверенный материал capture boundary.\n")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if withArtifact {
		payload.Artifacts = []command.CompletedArtifact{{FileName: "capture.md", MediaType: "text/markdown", Content: body, SizeBytes: int64(len(body)), SHA256: digest}}
	}
	// Every call reconstructs the external intent. Object preparation deliberately replaces
	// artifact bytes with private prepared receipts in its own invocation payload.
	// Каждый вызов заново восстанавливает внешний intent: подготовка объекта заменяет bytes
	// приватными prepared receipts внутри payload своего вызова.
	intent, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("encode original capture completion intent")
	}
	freshPayload := func() command.CompleteExecutionInput {
		t.Helper()
		var fresh command.CompleteExecutionInput
		if json.Unmarshal(intent, &fresh) != nil {
			t.Fatal("decode independent capture completion intent")
		}
		return fresh
	}
	completed := execute(command.CompleteExecution, "complete", nil, freshPayload())
	after := proof()
	if after.LeaseState != "COMPLETED" || after.Capture == nil || len(after.Capture) != 8 ||
		after.Capture["schema"] != "kodex.workflow-capture.v1" || after.Capture["runtimeRevisionId"] != before.RevisionID ||
		after.Capture["leaseId"] != before.LeaseID || after.Capture["turnId"] != before.TurnID ||
		after.Capture["runtimeAttempt"] != float64(before.RuntimeAttempt) || after.Capture["businessAttempt"] != float64(1) ||
		after.Capture["generation"] != float64(before.Generation) {
		t.Fatal("capture did not retain the exact producing runtime")
	}
	entries, ok := after.Capture["artifacts"].([]any)
	if !ok || len(entries) != len(payload.Artifacts) || after.ArtifactCount != int64(len(entries)) {
		t.Fatal("capture count or explicit empty batch mismatch")
	}
	if withArtifact {
		entry, ok := entries[0].(map[string]any)
		if !ok || len(entry) != 7 || len(completed.CreatedRefs) != 1 || entry["ref"] != completed.CreatedRefs[0] ||
			entry["digest"] != "sha256:"+digest || entry["sizeBytes"] != float64(len(body)) ||
			entry["fileName"] != "capture.md" || entry["mediaType"] != "text/markdown" || entry["revision"] != float64(1) || entry["revisionRef"] == "" {
			t.Fatal("capture did not retain exact immutable content pins")
		}
		reject("replace-batch", after)
	}
	for _, mutation := range []string{"clear", "runtime-attempt", "business-attempt", "rewind", "delete"} {
		reject(mutation, after)
	}
	replayed := execute(command.CompleteExecution, "complete", nil, freshPayload())
	if !reflect.DeepEqual(completed.CreatedRefs, replayed.CreatedRefs) || !reflect.DeepEqual(after, proof()) {
		t.Fatal("capture receipt replay changed durable provenance")
	}
	encoded, err := json.Marshal(after.Capture)
	if err != nil || len(encoded) > 32768 {
		t.Fatal("capture storage budget exceeded")
	}
	if !withArtifact {
		// No real runtime or storage resource is created by this fixture. Its only external
		// inventory entry may describe an absent PVC; external cleanup is not under test here.
		// Оснастка не создаёт настоящих runtime/storage ресурсов. Единственная допустимая
		// внешняя запись описывает отсутствующий PVC; внешняя очистка здесь не проверяется.
		current, err := service.GetProject(ctx, owner, project.Ref)
		if err != nil {
			t.Fatal("read project before isolated capture purge")
		}
		trashed := execute(command.TrashProject, "trash", &current.Version, command.ProjectLifecycleInput{Ref: project.Ref}).Project
		execute(command.PurgeProject, "purge", &trashed.Version, command.ProjectLifecycleInput{Ref: project.Ref})
		candidates, err := repository.ListPendingProjectPurges(ctx, 2)
		if err != nil || len(candidates) != 1 || candidates[0].ProjectRef != project.Ref {
			t.Fatal("exact capture purge candidate missing")
		}
		inventory, err := repository.ProjectPurgeInventory(ctx, candidates[0])
		if err != nil {
			t.Fatal("read isolated capture purge inventory")
		}
		for _, item := range inventory {
			if item.Kind != "PVC" {
				t.Fatal("unexpected external resource in empty capture fixture")
			}
		}
		if err := repository.FinalizeProjectPurge(ctx, candidates[0], inventory); err != nil {
			t.Fatal("purge captured execution through native owner boundary", err)
		}
		if err := repository.FinalizeProjectPurge(ctx, candidates[0], inventory); err != nil {
			t.Fatal("replay native captured execution purge")
		}
		var nodes, runs, targets, contexts int64
		var receipt string
		if err := pool.QueryRow(ctx, queryWorkflowCapturePurgeProof, pgx.StrictNamedArgs{
			"node_ref": nodeRef, "run_ref": run.Ref, "project_ref": project.Ref,
		}).Scan(&nodes, &runs, &targets, &contexts, &receipt); err != nil || nodes != 0 || runs != 0 || targets != 0 || contexts != 0 || receipt != "DONE" {
			t.Fatal("captured execution purge left rows or authorization context")
		}
	}
}
