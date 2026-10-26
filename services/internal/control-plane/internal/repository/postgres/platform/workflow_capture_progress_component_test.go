package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowCaptureProgressProof reads real owner state without preparing business rows or grants.
// queryWorkflowCaptureProgressProof читает настоящее owner state, не готовя business rows или grants.
//
//go:embed testdata/sql/workflow_capture_progress_proof.sql
var queryWorkflowCaptureProgressProof string

// workflowCaptureProgressNode keeps physical lineage separate from the logical accepted history.
// Execution coordinates reuse the existing native dispatch observer, never model-supplied pins.
// workflowCaptureProgressNode отделяет physical lineage от принятой logical history. Координаты
// исполнения используют existing native dispatch observer, а не pins из ответа модели.
type workflowCaptureProgressNode struct {
	ID, Ref, OriginNodeID, StepKey, AgentRef, State, RunRef, SessionID, TurnID, TurnRef string
	RuntimeAttempt, BusinessAttempt                                                     int32
	History                                                                             []entity.WorkflowAttemptResult
	Inputs                                                                              []workflowDispatchInput
	Capture                                                                             map[string]any
	Executions                                                                          []workflowDispatchExecution
}

// workflowCaptureProgressProof exposes stable read-only facts so replay cannot hide another node,
// run, turn, journal, manager or business event behind an otherwise identical result summary.
// workflowCaptureProgressProof передаёт устойчивые read-only факты, не позволяя replay скрыть другой
// node, run, turn, journal, manager или business event за тем же result summary.
type workflowCaptureProgressProof struct {
	RootID, State                                                                 string
	RunCount, NodeCount, Continues, EventCount, Journals, Managers, ClaimedLeases int64
	Nodes                                                                         []workflowCaptureProgressNode
}

// workflowCaptureProgressNodeByRef selects one exact physical node, rejecting ambiguity explicitly.
// workflowCaptureProgressNodeByRef выбирает один exact physical node, явно отклоняя неоднозначность.
func workflowCaptureProgressNodeByRef(t *testing.T, proof workflowCaptureProgressProof, ref string) workflowCaptureProgressNode {
	t.Helper()
	var selected workflowCaptureProgressNode
	count := 0
	for _, node := range proof.Nodes {
		if node.Ref == ref {
			selected, count = node, count+1
		}
	}
	if count != 1 {
		t.Fatal("capture progress physical node is missing or ambiguous")
	}
	return selected
}

// assertWorkflowCaptureProgressClaim binds the observed physical execution to the actual lease/RR
// returned by ClaimExecution, without guessing a generation or selecting by timestamp.
// assertWorkflowCaptureProgressClaim связывает physical execution с фактическими lease/RR из
// ClaimExecution, не угадывая поколение и не выбирая по времени.
func assertWorkflowCaptureProgressClaim(t *testing.T, proof workflowCaptureProgressProof, lease map[string]any) workflowCaptureProgressNode {
	t.Helper()
	node := workflowCaptureProgressNodeByRef(t, proof, stringMap(lease, "nodeRef"))
	if len(node.Executions) != 1 || node.State != "RUNNING" || len(node.History) != 0 || node.Capture != nil {
		t.Fatal("capture progress claim did not create one uncompleted execution")
	}
	execution := node.Executions[0]
	if node.RunRef != stringMap(lease, "runRef") || node.AgentRef != stringMap(lease, "agentRef") || node.TurnRef != stringMap(lease, "turnRef") ||
		execution.RevisionRef != stringMap(lease, "runtimeRevisionRef") || execution.LeaseRef != stringMap(lease, "leaseRef") ||
		execution.Generation != runtimeRevisionMapInt64(lease, "generation") || execution.NodeID != node.ID || execution.TurnID != node.TurnID ||
		execution.RootRunID != proof.RootID || execution.RuntimeAttempt != node.RuntimeAttempt || execution.LeaseState != "CLAIMED" {
		t.Fatal("capture progress claim lost exact node/turn/root/revision/lease pins")
	}
	return node
}

// workflowCaptureProgressArtifact prepares real completion bytes with their expected SHA256.
// It supplies no prepared storage receipt, antivirus verdict or accepted-result assertion.
// workflowCaptureProgressArtifact готовит реальные completion bytes с ожидаемым SHA256, не задавая
// prepared storage receipt, антивирусный verdict или утверждение о принятом результате.
func workflowCaptureProgressArtifact(name string, body []byte) command.CompletedArtifact {
	sum := sha256.Sum256(body)
	return command.CompletedArtifact{FileName: name, MediaType: "text/markdown", Content: body, SizeBytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}

// assertWorkflowCaptureProgressFile reads one exact RUN_RESULT descriptor and its owner-authorized
// bytes using native runtime-controller principals and the currently claimed lease/catalog.
// assertWorkflowCaptureProgressFile читает exact RUN_RESULT descriptor и owner-authorized bytes через
// native runtime-controller principals и текущие claimed lease/catalog.
func assertWorkflowCaptureProgressFile(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service,
	lease map[string]any, ref, name, digest string, revision int64, expected []byte,
) entity.ExecutionFileDescriptor {
	t.Helper()
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
	page, err := service.GetExecutionFileManifest(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "manifest"), execution, query.Page{Size: 100})
	if err != nil || len(page.Items) != 1 || page.Total != 1 || page.Next != "" || page.Catalog.Ref != execution.CatalogRef || page.Catalog.Digest != execution.CatalogDigest {
		t.Fatal("capture progress exact file catalog is unavailable", completionOutcomeClass(err))
	}
	file := page.Items[0]
	if file.ArtifactRef != ref || file.Name != name || file.Digest != digest || file.Revision != revision || file.SizeBytes != int64(len(expected)) || file.Source != "AGENT_RESULT" {
		t.Fatal("capture progress catalog substituted immutable file pins")
	}
	exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
	if _, err := service.GetExecutionFileMetadata(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "metadata"), execution, exact); err != nil {
		t.Fatal("capture progress file metadata denied", completionOutcomeClass(err))
	}
	reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller",
		Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	download, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, ref)
	if err != nil {
		t.Fatal("capture progress file bytes denied", completionOutcomeClass(err))
	}
	body, readErr := io.ReadAll(io.LimitReader(download.Reader, int64(len(expected))+1))
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(body, expected) {
		t.Fatal("capture progress read changed the exact admitted file bytes")
	}
	return file
}

// TestWorkflowCaptureProgressComponent drives a real two-step SERVER_DAG_V1 through explicit capture,
// a fresh physical continuation and EARLY artifact acceptance before the dependent stage can run.
// All writes use native owner/worker APIs; SQL observes only. Replays independently decode the same
// external completion bytes. No GitHub publication, model execution or full PDLC is claimed here.
// TestWorkflowCaptureProgressComponent исполняет настоящий SERVER_DAG_V1 из двух шагов: explicit
// capture, fresh physical continuation и приёмку EARLY artifact до допуска зависимого этапа. Все writes
// идут через native owner/worker APIs; SQL только наблюдает. Replay независимо декодирует исходные
// completion bytes. Проверка не заявляет GitHub publication, запуск модели или полный PDLC.
func TestWorkflowCaptureProgressComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated capture progress database")
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
			Mutation: value.Mutation{IdempotencyKey: "capture-progress-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native capture progress command: key=%s kind=%s outcome=%s", key, kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Native early capture", Language: "ru"}).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "progress-manager", "Менеджер исключений")
	agents := map[string]entity.Agent{}
	for _, key := range []string{"producer", "consumer"} {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "progress-"+key, key)
		agents[key] = *execute(command.ChangeAgentCapability, "artifact-"+key, &agent.Version,
			command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Capture и зависимый материал", Purpose: "Проверить EARLY capture без бизнес-приёмки.",
		CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{
			{Key: "producer", Position: 1, Name: "Источник", AgentRef: agents["producer"].Ref, Instructions: "Сохранить early.md, затем завершить шаг по его точным pins.", TimeoutSeconds: 120,
				RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "source", Kind: "ARTIFACT", FileName: "early.md", MediaType: "text/markdown"}}},
			{Key: "consumer", Position: 2, Name: "Потребитель", AgentRef: agents["consumer"].Ref, Instructions: "Прочитать принятый early.md и сохранить consumer.md.", TimeoutSeconds: 120, DependsOn: []string{"producer"},
				RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "consumer.md", MediaType: "text/markdown"}}},
		}}
	workflow := execute(command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Сохранить ранний материал и передать точную ревизию потребителю.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	read := func() workflowCaptureProgressProof {
		t.Helper()
		var raw []byte
		if err := pool.QueryRow(ctx, queryWorkflowCaptureProgressProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "project_ref": project.Ref, "manager_ref": manager.Ref}).Scan(&raw); err != nil {
			t.Fatal("read capture progress owner proof", completionOutcomeClass(err))
		}
		var proof workflowCaptureProgressProof
		if json.Unmarshal(raw, &proof) != nil || proof.Managers != 0 {
			t.Fatal("capture progress proof invalid or manager execution appeared")
		}
		return proof
	}
	claim := func(key, agentRef string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "capture-progress-fixture", Limit: 2}).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "agentRef") != agentRef {
			t.Fatal("capture progress admitted an unexpected execution")
		}
		return items[0]
	}
	complete := func(key string, payload command.CompleteExecutionInput) command.Result {
		t.Helper()
		intent, err := json.Marshal(payload)
		if err != nil {
			t.Fatal("encode capture progress original completion")
		}
		fresh := func() command.CompleteExecutionInput {
			var decoded command.CompleteExecutionInput
			if json.Unmarshal(intent, &decoded) != nil {
				t.Fatal("decode independent capture progress completion")
			}
			return decoded
		}
		result := execute(command.CompleteExecution, key, nil, fresh())
		before := read()
		replayed := execute(command.CompleteExecution, key, nil, fresh())
		if !reflect.DeepEqual(result.CreatedRefs, replayed.CreatedRefs) || !reflect.DeepEqual(before, read()) {
			t.Fatal("capture progress replay duplicated or changed owner state")
		}
		return result
	}
	first := claim("producer", agents["producer"].Ref)
	initial := read()
	producer := assertWorkflowCaptureProgressClaim(t, initial, first)
	if initial.State != "RUNNING" || initial.NodeCount != 2 || initial.Journals != 0 || producer.BusinessAttempt != 1 || producer.RuntimeAttempt != 1 || producer.OriginNodeID != producer.ID || len(producer.Executions) != 1 {
		t.Fatal("capture progress initial assignment is not a fresh business attempt")
	}
	earlyBody := []byte("Ранний материал: проверяется точная ревизия, а не новый HEAD.\n")
	artifact := workflowCaptureProgressArtifact("early.md", earlyBody)
	progress := complete("capture", command.CompleteExecutionInput{LeaseRef: stringMap(first, "leaseRef"), Fence: stringMap(first, "fence"), Generation: runtimeRevisionMapInt64(first, "generation"),
		Success: true, Usage: turnUsageFixture(), ResultSummary: `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["source"]}`, Artifacts: []command.CompletedArtifact{artifact}})
	afterCapture := read()
	if len(progress.CreatedRefs) != 1 || afterCapture.State != "RUNNING" || afterCapture.Journals != 0 || afterCapture.Continues != 1 || afterCapture.NodeCount != 3 || afterCapture.ClaimedLeases != 0 || afterCapture.RunCount != initial.RunCount {
		t.Fatal("capture progress accepted business work or failed to create one same-run continuation")
	}
	captured := workflowCaptureProgressNodeByRef(t, afterCapture, producer.Ref)
	entries, captureOK := captured.Capture["artifacts"].([]any)
	if !captureOK || len(entries) != 1 || captured.State != "SUCCEEDED" || len(captured.Executions) != 1 || captured.Executions[0].LeaseState != "COMPLETED" {
		t.Fatal("capture progress did not seal one real producing batch")
	}
	pin, pinOK := entries[0].(map[string]any)
	if !pinOK || pin["ref"] != progress.CreatedRefs[0] || pin["digest"] != "sha256:"+artifact.SHA256 || pin["fileName"] != artifact.FileName || pin["revisionRef"] == "" {
		t.Fatal("capture progress changed immutable producing artifact pins")
	}
	for _, node := range afterCapture.Nodes {
		if node.StepKey == "consumer" && (node.State != "PLANNED" || node.BusinessAttempt != 0 || len(node.Executions) != 0) {
			t.Fatal("dependent consumer started before producer business acceptance")
		}
	}
	continued := claim("producer-continuation", agents["producer"].Ref)
	continuation := assertWorkflowCaptureProgressClaim(t, read(), continued)
	if continuation.ID == producer.ID || continuation.OriginNodeID != producer.ID || continuation.BusinessAttempt != 1 || continuation.RuntimeAttempt != 2 || continuation.RunRef != producer.RunRef || continuation.SessionID != producer.SessionID || continuation.TurnID == producer.TurnID || len(continuation.Executions) != 1 || continuation.Executions[0].RevisionID == producer.Executions[0].RevisionID || continuation.Executions[0].LeaseID == producer.Executions[0].LeaseID {
		t.Fatal("capture progress reused physical execution or changed its business identity")
	}
	earlyFile := assertWorkflowCaptureProgressFile(t, ctx, repository, service, continued, progress.CreatedRefs[0], "early.md", "sha256:"+artifact.SHA256, 1, earlyBody)
	finalDeclaration, err := json.Marshal(entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{{Key: "source", Source: &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: earlyFile.ArtifactRef, Revision: earlyFile.Revision, Digest: earlyFile.Digest}}}})
	if err != nil {
		t.Fatal("encode exact early artifact declaration")
	}
	finalProducer := complete("producer-final", command.CompleteExecutionInput{LeaseRef: stringMap(continued, "leaseRef"), Fence: stringMap(continued, "fence"), Generation: runtimeRevisionMapInt64(continued, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: string(finalDeclaration)})
	accepted := read()
	anchor := workflowCaptureProgressNodeByRef(t, accepted, producer.Ref)
	finished := workflowCaptureProgressNodeByRef(t, accepted, continuation.Ref)
	finalEntries, finalCaptureOK := finished.Capture["artifacts"].([]any)
	if len(finalProducer.CreatedRefs) != 0 || accepted.State != "RUNNING" || accepted.Journals != 1 || len(anchor.History) != 1 || !finalCaptureOK || len(finalEntries) != 0 {
		t.Fatal("EARLY final completion did not preserve empty final capture and one business receipt")
	}
	receipt := anchor.History[0]
	if !receipt.Accepted || receipt.Reason != "" || receipt.Attempt != 1 || receipt.RunRef != continuation.RunRef || receipt.RuntimeRevisionID != continuation.Executions[0].RevisionID || receipt.Provenance == nil || receipt.Provenance.NodeID != continuation.ID || receipt.Provenance.OriginNodeID != producer.ID || receipt.Provenance.LeaseID != continuation.Executions[0].LeaseID || receipt.Provenance.RootRunID != accepted.RootID || receipt.Provenance.TurnID != continuation.TurnID || receipt.Provenance.RuntimeAttempt != 2 || receipt.Provenance.Generation != continuation.Executions[0].Generation || len(receipt.Results) != 1 {
		t.Fatal("EARLY acceptance lost final execution provenance")
	}
	evidence := receipt.Results[0]
	revision, err := service.GetArtifactRevision(ctx, owner, evidence.ArtifactRef, evidence.ArtifactRevision)
	if err != nil || evidence.ArtifactRef != earlyFile.ArtifactRef || evidence.ArtifactRevision != pin["revisionRef"] || evidence.Digest != artifact.SHA256 || revision.Revision != earlyFile.Revision || revision.Digest != earlyFile.Digest || revision.ScanState != "CLEAN" {
		t.Fatal("EARLY acceptance substituted the producing artifact revision")
	}
	consumerLease := claim("consumer", agents["consumer"].Ref)
	consumer := assertWorkflowCaptureProgressClaim(t, read(), consumerLease)
	if consumer.BusinessAttempt != 1 || len(consumer.Inputs) != 1 || consumer.Inputs[0].StepKey != "producer" || consumer.Inputs[0].SourceNodeID != producer.ID || !reflect.DeepEqual(consumer.Inputs[0].Receipt, receipt) {
		t.Fatal("consumer did not receive the exact accepted producer snapshot")
	}
	assertWorkflowCaptureProgressFile(t, ctx, repository, service, consumerLease, earlyFile.ArtifactRef, earlyFile.Name, earlyFile.Digest, earlyFile.Revision, earlyBody)
	consumerResult := complete("consumer-final", command.CompleteExecutionInput{LeaseRef: stringMap(consumerLease, "leaseRef"), Fence: stringMap(consumerLease, "fence"), Generation: runtimeRevisionMapInt64(consumerLease, "generation"), Success: true, Usage: turnUsageFixture(),
		ResultSummary: `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`,
		Artifacts:     []command.CompletedArtifact{workflowCaptureProgressArtifact("consumer.md", []byte("Проверенный результат потребителя.\n"))}})
	final := read()
	if final.State != "SUCCEEDED" || final.Journals != 2 || final.NodeCount != 3 || final.Continues != 1 || final.ClaimedLeases != 0 || final.RunCount != initial.RunCount+1 || len(consumerResult.CreatedRefs) != 1 {
		t.Fatal("accepted EARLY pipeline did not close the complete server DAG")
	}
	consumerFinal := workflowCaptureProgressNodeByRef(t, final, consumer.Ref)
	if len(consumerFinal.History) != 1 || !consumerFinal.History[0].Accepted || len(consumerFinal.History[0].Results) != 1 || consumerFinal.History[0].Results[0].ArtifactRef != consumerResult.CreatedRefs[0] {
		t.Fatal("consumer did not accept its own new artifact")
	}
	publicRun, graph, err := service.GetRunGraphSnapshot(ctx, owner, run.Ref)
	if err != nil || publicRun.State != "SUCCEEDED" {
		t.Fatal("owner graph did not expose completed EARLY pipeline")
	}
	publicAccepted := 0
	for _, node := range graph.Nodes {
		if node.AgentRef == manager.Ref && node.Type == "AGENT_EXECUTION" {
			t.Fatal("capture progress public graph contains a manager execution")
		}
		for _, attempt := range node.WorkflowResults {
			if !attempt.Accepted || attempt.RuntimeRevisionID != "" || attempt.Provenance != nil {
				t.Fatal("capture progress public result is unaccepted or exposes private pins")
			}
			publicAccepted++
		}
	}
	if publicAccepted != 2 {
		t.Fatal("owner graph lost accepted EARLY business results")
	}
}
