package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestWorkflowExceptionContextComponent delivers an actual source decision and two exact file
// bodies to the manager without manufacturing a Gate, episode, grant, capture or journal in SQL.
// TestWorkflowExceptionContextComponent передаёт менеджеру настоящее решение и два exact файла,
// не создавая Gate, episode, grant, capture либо journal через SQL.
func TestWorkflowExceptionContextComponent(t *testing.T) {
	for _, mode := range []string{"owner-cancel", "invalid-manager-complete", "owner-revoke", "owner-expired-lease"} {
		t.Run(mode, func(t *testing.T) { testWorkflowExceptionContext(t, mode) })
	}
}

// testWorkflowExceptionContext uses ordinary owner commands for the complete two-step lifecycle;
// only existing SELECT observers inspect durable provenance. Each mode owns an isolated database.
// testWorkflowExceptionContext выполняет lifecycle двух шагов обычными owner-командами;
// durable provenance читают только существующие SELECT observers. Каждый режим изолирует БД.
func testWorkflowExceptionContext(t *testing.T, mode string) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated exception context database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	project := f.execute(command.CreateProject, "context-project", nil, command.ProjectInput{Name: "Exact exception context", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "context-manager", "Менеджер")
	producer := createLifecycleAgent(t, ctx, service, owner, project.Ref, "context-producer", "Автор")
	producer = *f.execute(command.ChangeAgentCapability, "context-artifacts", &producer.Version, command.AgentBindingInput{AgentRef: producer.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Данные исключения", Purpose: "Передать точные данные, не полномочия.", CoordinatorAgentRef: manager.Ref, Concurrency: 1, TimeoutSeconds: 180, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{
		{Key: "analysis", Position: 1, Name: "Анализ", AgentRef: producer.Ref, Instructions: "Создать a.md.", TimeoutSeconds: 90, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "a.md", MediaType: "text/markdown"}}},
		{Key: "review", Position: 2, Name: "Проверка", AgentRef: producer.Ref, DependsOn: []string{"analysis"}, Instructions: "Создать b.md.", TimeoutSeconds: 90, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REQUEST_CHANGES", "REJECT", "CANCEL"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "b.md", MediaType: "text/markdown"}}},
	}}
	workflow := f.execute(command.CreateWorkflow, "context-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "context-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "context-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	initiator := owner
	var fileBinding *entity.AccessBinding
	if mode == "owner-revoke" {
		var subject string
		initiator, subject = previewAuthorityActor(t, ctx, repository, service, owner, "context-limited-initiator", []string{"project.view", "agent.view", "agent.launch", "workflow.view", "workflow.launch", "run.view", "artifact.upload"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
		fileBinding = previewAuthorityGrant(t, ctx, service, owner, "context-files", subject, []string{"artifact.view", "artifact.download"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	}
	launch, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: initiator, Mutation: value.Mutation{IdempotencyKey: "context-launch"}, Payload: command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Подготовить и проверить документы."}})
	if err != nil || launch.Run == nil {
		t.Fatal("launch native context workflow", completionOutcomeClass(err))
	}
	root := launch.Run
	f.runRef = root.Ref
	for index, name := range []string{"a.md", "b.md"} {
		claims := f.claims("context-claim-" + name)
		if len(claims) != 1 || stringMap(claims[0], "agentRef") != producer.Ref {
			t.Fatal("context fixture did not claim exact published producer")
		}
		payload := command.CompleteExecutionInput{LeaseRef: stringMap(claims[0], "leaseRef"), Fence: stringMap(claims[0], "fence"), Generation: runtimeRevisionMapInt64(claims[0], "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`, Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact(name, []byte("Точные данные "+name+"\n"))}}
		if mode == "invalid-manager-complete" && index == 1 {
			payload.ResultSummary = "Not a closed result declaration; the captured file remains data."
		}
		// Replay independently decodes the same wire bytes; owner preparation may mutate its private artifact slice.
		// Replay независимо декодирует те же wire bytes; owner preparation может менять свой private artifact slice.
		wire, err := json.Marshal(payload)
		if err != nil {
			t.Fatal("encode immutable context completion intent")
		}
		complete := func() {
			t.Helper()
			var fresh command.CompleteExecutionInput
			if json.Unmarshal(wire, &fresh) != nil {
				t.Fatal("decode immutable context completion intent")
			}
			f.execute(command.CompleteExecution, "context-complete-"+name, nil, fresh)
		}
		complete()
		before := f.read()
		complete()
		if !reflect.DeepEqual(before, f.read()) {
			t.Fatal("completion replay duplicated context lineage")
		}
	}
	var sourceGate entity.OwnerGate
	const comment = "Исправить AR01 и SEC01.\nСохранить смысл: «точные данные», без повторного UNKNOWN."
	if mode != "invalid-manager-complete" {
		current, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil || len(current.GateRefs) != 1 {
			t.Fatal("native context result gate missing")
		}
		sourceGate, err = service.GetOwnerGate(ctx, owner, current.GateRefs[0])
		if err != nil || sourceGate.State != "OPEN" {
			t.Fatal("native context source gate not open")
		}
		decision := command.GateResolutionInput{GateRef: sourceGate.Ref, Decision: "REQUEST_CHANGES", Comment: comment}
		f.execute(command.ResolveOwnerGate, "context-request", &sourceGate.Version, decision)
		before := f.read()
		f.execute(command.ResolveOwnerGate, "context-request", &sourceGate.Version, decision)
		if !reflect.DeepEqual(before, f.read()) || before.managers != 1 {
			t.Fatal("owner replay created a second manager")
		}
	}
	claims := f.claims("context-manager")
	if len(claims) != 1 || stringMap(claims[0], "agentRef") != manager.Ref {
		t.Fatal("context manager was not admitted")
	}
	lease := claims[0]
	capabilities := runtimeRevisionStringSlice(lease["capabilities"])
	if len(capabilities) != 1 || capabilities[0] != runtimecontract.ArtifactCapability || len(runtimeRevisionGrants(lease["integrationGrants"])) != 0 {
		t.Fatal("exception context widened manager execution authority")
	}
	parts := strings.SplitN(stringMap(lease, "task"), "Owner-projected exception context (data):\n", 2)
	var evidence workflowExceptionContext
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &evidence) != nil || evidence.StepKey != "review" || len(evidence.Inputs) != 1 || evidence.Inputs[0].StepKey != "analysis" {
		t.Fatal("actual manager task lost the source and exact predecessor context")
	}
	if mode == "invalid-manager-complete" {
		if evidence.Reason != "RESULT_INVALID" || evidence.OwnerDecision != nil {
			t.Fatal("format rejection fabricated an owner decision")
		}
	} else if evidence.OwnerDecision == nil || evidence.OwnerDecision.GateRef != sourceGate.Ref || evidence.OwnerDecision.GateVersion != sourceGate.Version+1 || evidence.OwnerDecision.Decision != "REQUEST_CHANGES" || evidence.OwnerDecision.Comment != comment {
		t.Fatal("manager did not receive the exact stored source decision")
	}
	execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
	manifestActor := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	page, err := service.GetExecutionFileManifest(ctx, manifestActor, execution, query.Page{Size: 100})
	if err != nil || page.Total != 2 || len(page.Items) != 2 || page.Next != "" {
		t.Fatal("manager catalog does not contain exactly source and dependency files", completionOutcomeClass(err))
	}
	metadataActor := runtimeFilesTestPrincipal(t, ctx, repository, "metadata")
	for _, file := range page.Items {
		if file.Name != "a.md" && file.Name != "b.md" {
			t.Fatal("manager received an unrelated file")
		}
		expected := []byte("Точные данные " + file.Name + "\n")
		digest := sha256.Sum256(expected)
		if file.Digest != "sha256:"+hex.EncodeToString(digest[:]) || file.Revision != 1 {
			t.Fatal("manager catalog changed exact producing content pins")
		}
		exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
		if _, err := service.GetExecutionFileMetadata(ctx, metadataActor, execution, exact); err != nil {
			t.Fatal("manager exact metadata denied", completionOutcomeClass(err))
		}
		preview, err := service.PreviewExecutionFile(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "preview"), execution, exact, 16384)
		if err != nil || preview.Truncated || preview.Text != string(expected) {
			t.Fatal("manager exact preview bytes changed", completionOutcomeClass(err))
		}
		reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
		download, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef)
		if err != nil {
			t.Fatal("manager source bytes denied", completionOutcomeClass(err))
		}
		body, readErr := io.ReadAll(io.LimitReader(download.Reader, int64(len(expected))+1))
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(body, expected) {
			t.Fatal("manager source bytes changed")
		}
		forged := exact
		forged.Revision++
		if _, err := service.GetExecutionFileMetadata(ctx, metadataActor, execution, forged); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("undeclared revision was accepted", completionOutcomeClass(err))
		}
		if mode == "owner-cancel" && file.Name == "b.md" {
			workflowExceptionAdvanceRevision(t, f, file)
			old, err := service.PreviewExecutionFile(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "preview"), execution, exact, 16384)
			if err != nil || old.Text != string(expected) || old.Metadata.File.Revision != file.Revision {
				t.Fatal("new owner revision displaced the exact frozen producer body", completionOutcomeClass(err))
			}
		}
	}
	if mode == "owner-revoke" {
		f.execute(command.RevokeAccessBinding, "context-revoke", &fileBinding.Version, command.AccessBindingInput{BindingRef: fileBinding.Ref})
		visible, err := service.GetExecutionFileManifest(ctx, manifestActor, execution, query.Page{Size: 100})
		if err != nil || visible.Total != 0 || len(visible.Items) != 0 || visible.Catalog.Digest != page.Catalog.Digest {
			t.Fatal("fresh source ACL did not close frozen manager entries", completionOutcomeClass(err))
		}
		for _, file := range page.Items {
			exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
			if _, err := service.GetExecutionFileMetadata(ctx, metadataActor, execution, exact); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("revoked exact file metadata remained readable", completionOutcomeClass(err))
			}
			if _, err := service.PreviewExecutionFile(ctx, runtimeFilesTestPrincipal(t, ctx, repository, "preview"), execution, exact, 16384); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("revoked exact file preview remained readable", completionOutcomeClass(err))
			}
			reader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
			if _, err := service.ReadExecutionArtifact(ctx, reader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("revoked exact file bytes remained readable", completionOutcomeClass(err))
			}
		}
	} else if mode == "owner-expired-lease" {
		// Canonical leases last thirty seconds; no database clock or lease row is rewritten.
		// Канонический lease живёт тридцать секунд; часы БД и строки lease не меняются.
		timer := time.NewTimer(31 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal("context lease expiry budget exceeded")
		}
		if _, err := service.GetExecutionFileManifest(ctx, manifestActor, execution, query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("expired manager lease retained file access", completionOutcomeClass(err))
		}
		fresh := f.claims("context-reclaim")
		if len(fresh) != 1 || stringMap(fresh[0], "nodeRef") != stringMap(lease, "nodeRef") || stringMap(fresh[0], "leaseRef") == stringMap(lease, "leaseRef") || runtimeRevisionMapInt64(fresh[0], "generation") <= runtimeRevisionMapInt64(lease, "generation") {
			t.Fatal("manager recovery did not create a fresh fenced lease")
		}
		freshCatalog := runtimeFilesTestContext(t, fresh[0], runtimecontract.FilePurposeRunResult)
		if current, err := service.GetExecutionFileManifest(ctx, manifestActor, freshCatalog, query.Page{Size: 100}); err != nil || len(current.Items) != 2 {
			t.Fatal("fresh manager generation lost exact context files", completionOutcomeClass(err))
		}
		lease, execution = fresh[0], freshCatalog
	} else if mode == "invalid-manager-complete" {
		f.complete("context-manager-finished", lease, "", false)
		if _, err := service.GetExecutionFileManifest(ctx, manifestActor, execution, query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("completed manager retained file access", completionOutcomeClass(err))
		}
	}
	current, err := service.GetRun(ctx, owner, root.Ref)
	if err != nil {
		t.Fatal("read context root for cancellation")
	}
	f.execute(command.CancelRun, "context-cancel", &current.Version, command.RunCommandInput{RunRef: root.Ref})
	if _, err := service.GetExecutionFileManifest(ctx, manifestActor, execution, query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("cancelled manager retained file access", completionOutcomeClass(err))
	}
	history, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
	if err != nil || history.Task != stringMap(lease, "task") {
		t.Fatal("context cancellation erased the immutable manager task")
	}
	if len(f.claims("context-no-resurrection")) != 0 {
		t.Fatal("cancelled context re-admitted model work")
	}
}

// workflowExceptionAdvanceRevision creates a distinct revision through the actual project assistant
// proposal, owner validation and apply path. It never inserts revision/content rows or supplies a scan receipt.
// workflowExceptionAdvanceRevision создаёт отдельную ревизию через настоящий proposal помощника,
// owner validation и apply. Helper не вставляет revision/content строки и не задаёт scan receipt.
func workflowExceptionAdvanceRevision(t *testing.T, f *workflowRecoveryRegression, file entity.ExecutionFileDescriptor) {
	t.Helper()
	profile := f.execute(command.CreateProjectAssistant, "context-revision-helper", nil, command.ProjectAssistantInput{ProjectRef: f.projectRef, Name: "Проверка версии", Purpose: "Создать одну новую ревизию с подтверждением владельца.", Instructions: "Не менять исходную историю."}).ProjectAssistant
	if profile == nil {
		t.Fatal("native revision helper missing")
	}
	agent, err := f.service.GetAgent(f.ctx, f.owner, profile.AgentRef)
	if err != nil {
		t.Fatal("read native revision helper")
	}
	f.execute(command.ChangeAgentCapability, "context-revision-capability", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true})
	conversation := f.execute(command.CreateAssistantConversation, "context-revision-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: f.projectRef, Context: entity.AssistantContextDescriptor{EntityKind: "ARTIFACT", EntityRef: file.ArtifactRef}}).Conversation
	f.execute(command.AddAssistantTurn, "context-revision-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Сохранить отдельную новую версию этого материала.", DeliveryMode: "QUEUE"})
	var lease map[string]any
	for _, candidate := range f.claims("context-revision-claim") {
		if stringMap(candidate, "sessionRef") == conversation.SessionRef {
			lease = candidate
		}
	}
	if lease == nil {
		t.Fatal("native revision helper did not claim its own turn")
	}
	operation := entity.AssistantPlanOperation{Key: "new-version", Type: createProjectFileRevision, Title: "Отдельная версия", Summary: "Не подменять ранее принятый файл.", Parameters: map[string]any{"artifactRef": file.ArtifactRef, "mediaType": "text/markdown", "content": "Иная версия после manager snapshot.\n"}}
	plan := executeWorkerAssistantPlan(t, f.ctx, f.service, f.worker, lease, "context-revision-proposal", operation).Plan
	if plan == nil {
		t.Fatal("native revision proposal missing")
	}
	validated := f.execute(command.ValidateAssistantPlan, "context-revision-validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}).Plan
	if validated == nil || validated.State != "VALID" {
		t.Fatal("native revision proposal did not validate")
	}
	applied := f.execute(command.ApplyAssistantPlan, "context-revision-apply", &validated.Version, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
	if applied.PlanReceipt == nil || applied.PlanReceipt.Outcome != "APPLIED" || len(applied.PlanReceipt.Operations) != 1 || applied.PlanReceipt.Operations[0].ArtifactRevision == nil || applied.PlanReceipt.Operations[0].ArtifactRevision.Revision != file.Revision+1 {
		t.Fatal("native revision apply did not produce one distinct receipt")
	}
	f.complete("context-revision-finished", lease, "", false)
}
